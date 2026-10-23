package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func NewChatCompletionsProxy() http.HandlerFunc {
	target := os.Getenv("NEXT_BACKEND_URL")
	if target == "" {
		target = "http://127.0.0.1:20128"
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		targetURL, _ = url.Parse("http://127.0.0.1:20128")
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		if !req.URL.IsAbs() {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
		}
	}

	proxy.FlushInterval = -1

	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}
}
