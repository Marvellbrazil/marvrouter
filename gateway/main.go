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
	"strings"
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

func createNextJsCmd(ctx context.Context, internalPort int) (*exec.Cmd, string, error) {
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

	nodePaths := []string{
		filepath.Join(os.Getenv("HOME"), ".9router", "runtime", "node_modules"),
		"/home/webserver-01/.9router/runtime/node_modules",
		filepath.Join(appDir, "node_modules"),
		"/usr/lib/node_modules/9router/app/node_modules",
	}
	if existing := os.Getenv("NODE_PATH"); existing != "" {
		nodePaths = append(nodePaths, existing)
	}

	var validPaths []string
	for _, p := range nodePaths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			validPaths = append(validPaths, p)
		}
	}

	args := []string{
		"--dns-result-order=ipv4first",
		"--max-old-space-size=4096",
		serverJs,
	}

	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Dir = appDir

	env := append(os.Environ(),
		fmt.Sprintf("PORT=%d", internalPort),
		"HOSTNAME=127.0.0.1",
		"NODE_ENV=production",
	)
	if len(validPaths) > 0 {
		env = append(env, fmt.Sprintf("NODE_PATH=%s", strings.Join(validPaths, string(os.PathListSeparator))))
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd, appDir, nil
}

func superviseNextJs(ctx context.Context, internalPort int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cmd, _, err := createNextJsCmd(ctx, internalPort)
		if err != nil {
			log.Printf("Internal Next.js config error: %v, retrying in 3s...", err)
			time.Sleep(3 * time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to spawn internal Next.js: %v, retrying in 3s...", err)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("Internal Next.js process running (PID: %d) on port %d", cmd.Process.Pid, internalPort)
		_ = cmd.Wait()

		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("Internal Next.js process on port %d exited. Restarting in 1s...", internalPort)
		time.Sleep(1 * time.Second)
	}
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

		log.Printf("Spawning supervised Next.js process on ephemeral port %d...", internalPort)
		go superviseNextJs(ctx, internalPort)

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
			log.Printf("Warning: Next.js internal process on port %d did not respond to healthcheck within 20s, continuing with fallback available...", internalPort)
		} else {
			log.Printf("Next.js internal UI process is ready on port %d", internalPort)
		}
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
		v1.Get("/quota/{uuid}", handlers.QuotaByID)
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
