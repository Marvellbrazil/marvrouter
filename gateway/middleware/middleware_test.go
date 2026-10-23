package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"9router-gateway/middleware"
)

func TestExtractApiKeyAndPeerToken(t *testing.T) {
	os.Setenv("NINEROUTER_PEER_TOKEN", "secret-test-token")
	defer os.Unsetenv("NINEROUTER_PEER_TOKEN")

	handler := middleware.PeerToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	reqNoToken := httptest.NewRequest("GET", "/v1/test", nil)
	rrNoToken := httptest.NewRecorder()
	handler.ServeHTTP(rrNoToken, reqNoToken)
	if rrNoToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on missing peer token, got %d", rrNoToken.Code)
	}

	reqValidToken := httptest.NewRequest("GET", "/v1/test", nil)
	reqValidToken.Header.Set("x-9r-peer-token", "secret-test-token")
	rrValidToken := httptest.NewRecorder()
	handler.ServeHTTP(rrValidToken, reqValidToken)
	if rrValidToken.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid peer token, got %d", rrValidToken.Code)
	}
}
