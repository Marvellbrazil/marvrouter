package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"9router-gateway/handlers"
	"9router-gateway/middleware"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("GATEWAY_PORT")
	}
	if port == "" {
		port = "20129"
	}
	if port[0] != ':' {
		port = ":" + port
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

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(middleware.Auth)

		v1.Get("/models", handlers.Models)
		v1.Get("/quota", handlers.Quota)
		v1.Post("/chat/completions", handlers.NewChatCompletionsProxy())
	})

	addr := port
	log.Printf("Starting 9router gateway on %s\n", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}
