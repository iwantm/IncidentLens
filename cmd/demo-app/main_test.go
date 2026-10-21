package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndVersionHandlers(t *testing.T) {
	tests := []struct {
		path    string
		want    string
		handler http.HandlerFunc
	}{
		{path: "/health", want: "Health check passed", handler: health},
		{path: "/version", want: "Version 1.0.0", handler: version},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			tt.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != http.StatusOK || response.Body.String() != tt.want {
				t.Fatalf("response = %d %q, want 200 %q", response.Code, response.Body.String(), tt.want)
			}
		})
	}
}

func TestWorkHandlerFailureSwitch(t *testing.T) {
	t.Run("fails when enabled", func(t *testing.T) {
		t.Setenv("FAIL_MODE", "true")
		response := httptest.NewRecorder()
		work(response, httptest.NewRequest(http.MethodGet, "/work", nil))
		if response.Code != http.StatusInternalServerError || response.Body.String() != "Worker failed" {
			t.Fatalf("response = %d %q, want 500 Worker failed", response.Code, response.Body.String())
		}
	})

	t.Run("succeeds otherwise", func(t *testing.T) {
		t.Setenv("FAIL_MODE", "false")
		response := httptest.NewRecorder()
		work(response, httptest.NewRequest(http.MethodGet, "/work", nil))
		if response.Code != http.StatusOK || response.Body.String() != "Worker is running" {
			t.Fatalf("response = %d %q, want 200 Worker is running", response.Code, response.Body.String())
		}
	})
}
