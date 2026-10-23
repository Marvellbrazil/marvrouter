package middleware

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"9router-gateway/db"
)

type ErrorResponse struct {
	Error ErrorDetails `json:"error"`
}

type ErrorDetails struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeUnauthorized(w http.ResponseWriter, message string, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	resp := ErrorResponse{
		Error: ErrorDetails{
			Message: message,
			Type:    "authentication_error",
			Code:    code,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func extractApiKey(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	if key := r.Header.Get("x-api-key"); key != "" {
		return strings.TrimSpace(key)
	}
	if key := r.Header.Get("x-goog-api-key"); key != "" {
		return strings.TrimSpace(key)
	}
	if key := r.URL.Query().Get("key"); key != "" {
		return strings.TrimSpace(key)
	}
	return ""
}

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		settings, err := db.GetSettings()
		if err != nil {
			writeUnauthorized(w, "Failed to load gateway settings", "settings_error")
			return
		}

		key := extractApiKey(r)
		if !settings.RequireApiKey && key == "" {
			next.ServeHTTP(w, r)
			return
		}

		if key == "" {
			writeUnauthorized(w, "Missing API key", "missing_api_key")
			return
		}

		valid, err := db.ValidateApiKey(key)
		if err != nil {
			writeUnauthorized(w, "Internal database error validating API key", "db_error")
			return
		}

		if !valid {
			writeUnauthorized(w, "Invalid or inactive API key", "invalid_api_key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func PeerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedToken := os.Getenv("NINEROUTER_PEER_TOKEN")
		if expectedToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		providedToken := r.Header.Get("x-9r-peer-token")
		if providedToken != expectedToken {
			writeUnauthorized(w, "Invalid trusted peer token", "invalid_peer_token")
			return
		}

		next.ServeHTTP(w, r)
	})
}
