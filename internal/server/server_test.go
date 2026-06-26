package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
