package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"9router-gateway/handlers"
	"9router-gateway/middleware"
)

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func startNextJsProcess(ctx context.Context, internalPort int) (*exec.Cmd, error) {
	appDir := os.Getenv("NEXT_APP_DIR")
	if appDir == "" {
		execPath, err := os.Executable()
		if err == nil {
			candidate := filepath.Join(filepath.Dir(execPath), "..", "cli", "app")
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				appDir = candidate
			}
		}
	}
	if appDir == "" {
		candidates := []string{
			"/home/webserver-01/projects/marvrouter/cli/app",
			"/usr/lib/node_modules/9router/app",
			"./cli/app",
			"../cli/app",
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				appDir = c
				break
			}
		}
	}

	serverJs := filepath.Join(appDir, "custom-server.js")
	if _, err := os.Stat(serverJs); os.IsNotExist(err) {
		serverJs = filepath.Join(appDir, "server.js")
	}

	cmd := exec.CommandContext(ctx, "node", serverJs)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", internalPort),
		"HOSTNAME=127.0.0.1",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	checkURL := fmt.Sprintf("http://127.0.0.1:%d/api/version", internalPort)
	ready := false
	for i := 0; i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get(checkURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusTemporaryRedirect {
				ready = true
				break
			}
		}
	}

	if !ready {
		log.Printf("Warning: Next.js internal process on port %d did not respond to healthcheck within 20s, continuing anyway...", internalPort)
	} else {
		log.Printf("Next.js internal UI process is ready on port %d", internalPort)
	}

	return cmd, nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("GATEWAY_PORT")
	}
	if port == "" {
		port = "20130"
	}
	if port[0] != ':' {
		port = ":" + port
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received termination signal, shutting down...")
		cancel()
	}()

	backendURL := os.Getenv("NEXT_BACKEND_URL")
	if backendURL == "" {
		internalPort, err := findFreePort()
		if err != nil {
			log.Fatalf("Failed to find free ephemeral port for Next.js: %v", err)
		}
		backendURL = fmt.Sprintf("http://127.0.0.1:%d", internalPort)
		_ = os.Setenv("NEXT_BACKEND_URL", backendURL)

		log.Printf("Spawning internal Next.js process on ephemeral port %d...", internalPort)
		cmd, err := startNextJsProcess(ctx, internalPort)
		if err != nil {
			log.Fatalf("Failed to spawn internal Next.js server: %v", err)
		}
		defer func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}()
	}

	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.Cors())

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/__shim_health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"shim":"go-native-gateway"}`))
	})

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(middleware.Auth)

		v1.Get("/models", handlers.Models)
		v1.Get("/quota", handlers.Quota)
		v1.Post("/chat/completions", handlers.NewChatCompletionsProxy())
		v1.Post("/messages", handlers.MessagesHandler())
	})

	r.NotFound(handlers.NewCatchAllReverseProxy())

	server := &http.Server{
		Addr:    port,
		Handler: r,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("Starting unified marvrouter master gateway on %s (backend: %s)\n", port, backendURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}
