package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"zensu/internal/api"
	"zensu/internal/kwik"
)

func TestRouterMiddlewareAndEndpoints(t *testing.T) {
	// Create router
	router := NewRouter(nil, nil, nil)

	t.Run("CORS OPTIONS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/search", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", rec.Code)
		}
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
			t.Errorf("expected CORS origin *, got %q", origin)
		}
	})

	t.Run("Missing query parameter q", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status BadRequest, got %d", rec.Code)
		}
	})

	t.Run("Missing slug parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/episodes", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status BadRequest, got %d", rec.Code)
		}
	})
}

func TestRewriteM3U8(t *testing.T) {
	u, _ := url.Parse("https://vault-08.uwucdn.top/stream/playlist.m3u8")
	input := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="key.key"
#EXTINF:6.0,
segment-1.jpg
#EXTINF:6.0,
https://vault-08.uwucdn.top/stream/segment-2.jpg`

	expected := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="http://localhost:8080/api/stream?proxy_url=https%3A%2F%2Fvault-08.uwucdn.top%2Fstream%2Fkey.key"
#EXTINF:6.0,
http://localhost:8080/api/stream?proxy_url=https%3A%2F%2Fvault-08.uwucdn.top%2Fstream%2Fsegment-1.jpg
#EXTINF:6.0,
http://localhost:8080/api/stream?proxy_url=https%3A%2F%2Fvault-08.uwucdn.top%2Fstream%2Fsegment-2.jpg`

	result := rewriteM3U8(input, u, "localhost:8080")
	if result != expected {
		t.Errorf("rewritten m3u8 does not match expected.\nGot:\n%s\n\nExpected:\n%s", result, expected)
	}
}

func TestIsExpiredError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{http.ErrHandlerTimeout, false},
		{&url.Error{Op: "Get", URL: "http://example.com", Err: http.ErrHandlerTimeout}, false},
		{fmt.Errorf("403 Forbidden — CF blocked, refresh cookies"), true},
		{fmt.Errorf("403 from kwik — CF blocked"), true},
		{fmt.Errorf("HTTP 403"), true},
		{fmt.Errorf("Cloudflare clearance expired"), true},
		{fmt.Errorf("forbidden"), true},
	}

	for _, tt := range tests {
		got := IsExpiredError(tt.err)
		if got != tt.expected {
			t.Errorf("IsExpiredError(%v) = %v, expected %v", tt.err, got, tt.expected)
		}
	}
}

func TestServerRefreshCredentials(t *testing.T) {
	refreshCount := 0
	srv := NewServer(nil, nil, nil, func() (*api.Client, *kwik.Extractor, error) {
		refreshCount++
		return nil, nil, nil
	})

	if err := srv.RefreshCredentials(); err != nil {
		t.Fatalf("unexpected error from RefreshCredentials: %v", err)
	}

	if refreshCount != 1 {
		t.Fatalf("expected refreshCount to be 1, got %d", refreshCount)
	}
}

