package handlers

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func NewChatCompletionsProxy() http.HandlerFunc {
	target := os.Getenv("NEXT_BACKEND_URL")
	if target == "" {
		target = "http://127.0.0.1:20128"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		upstreamURL := target + r.URL.Path
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
		}

		upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(bodyBytes))
		if err != nil {
			http.Error(w, "Failed to construct upstream request", http.StatusInternalServerError)
			return
		}

		for k, v := range r.Header {
			lower := strings.ToLower(k)
			if lower == "content-length" || lower == "host" || lower == "accept-encoding" {
				continue
			}
			upstreamReq.Header[k] = v
		}

		client := &http.Client{
			Timeout: 0,
		}

		resp, err := client.Do(upstreamReq)
		if err != nil {
			http.Error(w, "Upstream connection failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			w.Header()[k] = v
		}

		contentType := resp.Header.Get("Content-Type")
		isSSE := strings.Contains(strings.ToLower(contentType), "text/event-stream")

		if !isSSE {
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		w.Header().Set("x-shim", "9router-go-keepalive")
		w.WriteHeader(resp.StatusCode)
		flusher.Flush()

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-done:
					return
				case <-r.Context().Done():
					return
				case <-ticker.C:
					_, _ = w.Write([]byte(":\n\n"))
					flusher.Flush()
				}
			}
		}()

		reader := bufio.NewReader(resp.Body)
		buf := make([]byte, 4096)
		for {
			n, rErr := reader.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
				flusher.Flush()
			}
			if rErr != nil {
				break
			}
		}
		close(done)
	}
}
