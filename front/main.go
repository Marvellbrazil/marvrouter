package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type upstreamStore struct {
	raw atomic.Value
	url atomic.Value
}

func (s *upstreamStore) current(file string) *url.URL {
	data, err := os.ReadFile(file)
	if err != nil {
		if existing, ok := s.url.Load().(*url.URL); ok && existing != nil {
			return existing
		}
		return nil
	}
	raw := strings.TrimSpace(string(data))
	if raw == s.raw.Load() {
		if existing, ok := s.url.Load().(*url.URL); ok && existing != nil {
			return existing
		}
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Printf("Invalid upstream URL in %s: %q", file, raw)
		if existing, ok := s.url.Load().(*url.URL); ok && existing != nil {
			return existing
		}
		return nil
	}
	if old := s.raw.Load(); old != raw {
		log.Printf("Upstream changed: %s -> %s", old, raw)
	}
	s.raw.Store(raw)
	s.url.Store(target)
	return target
}

func main() {
	port := os.Getenv("FRONT_PORT")
	if port == "" {
		port = "20130"
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	upstreamFile := os.Getenv("UPSTREAM_FILE")
	if upstreamFile == "" {
		upstreamFile = "/home/webserver-01/.9router/front-upstream"
	}

	store := &upstreamStore{}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			target := store.current(upstreamFile)
			if target == nil {
				return
			}
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			if _, ok := req.Header["User-Agent"]; !ok {
				req.Header.Set("User-Agent", "")
			}
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			log.Printf("proxy error: %v", err)
			http.Error(w, `{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"backend unreachable"}}`, http.StatusBadGateway)
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__front/health", func(w http.ResponseWriter, r *http.Request) {
		target := store.current(upstreamFile)
		raw := ""
		if target != nil {
			raw = target.String()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"upstream":%q}`, raw)
	})
	mux.Handle("/", proxy)

	server := &http.Server{
		Addr:              port,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	log.Printf("marvrouter front proxy on %s (upstream file: %s)", port, upstreamFile)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "front proxy failed: %v\n", err)
		os.Exit(1)
	}
}
