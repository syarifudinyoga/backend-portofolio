package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseURLUsesEncodedCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "portfolio user")
	t.Setenv("POSTGRES_PASSWORD", "p@ss:/word?&")
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_DB", "portfolio data")
	t.Setenv("POSTGRES_SSLMODE", "require")

	config, err := pgxpool.ParseConfig(databaseURL())
	if err != nil {
		t.Fatalf("parse generated database URL: %v", err)
	}
	if config.ConnConfig.User != "portfolio user" {
		t.Errorf("user = %q, want %q", config.ConnConfig.User, "portfolio user")
	}
	if config.ConnConfig.Password != "p@ss:/word?&" {
		t.Errorf("password = %q, want credentials to survive URL encoding", config.ConnConfig.Password)
	}
	if config.ConnConfig.Host != "db.internal" || config.ConnConfig.Port != 5433 {
		t.Errorf("address = %s:%d, want db.internal:5433", config.ConnConfig.Host, config.ConnConfig.Port)
	}
	if config.ConnConfig.Database != "portfolio data" {
		t.Errorf("database = %q, want %q", config.ConnConfig.Database, "portfolio data")
	}
	if config.ConnConfig.TLSConfig == nil {
		t.Fatal("TLSConfig is nil, want TLS enabled for sslmode=require")
	}
}

func TestDatabaseRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: time.Second},
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 4, want: 8 * time.Second},
		{attempt: 5, want: 16 * time.Second},
		{attempt: 6, want: 30 * time.Second},
		{attempt: 20, want: 30 * time.Second},
	}

	for _, test := range tests {
		if got := databaseRetryDelay(test.attempt); got != test.want {
			t.Errorf("databaseRetryDelay(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestOriginMatches(t *testing.T) {
	tests := []struct {
		pattern string
		origin  string
		want    bool
	}{
		{pattern: "*", origin: "https://example.com", want: true},
		{pattern: "", origin: "https://example.com", want: true},
		{pattern: "https://example.com", origin: "https://example.com", want: true},
		{pattern: "https://example.com", origin: "HTTPS://EXAMPLE.COM", want: true},
		{pattern: "https://example.com", origin: "https://other.com", want: false},
		{pattern: "*.example.com", origin: "https://sub.example.com", want: true},
		{pattern: "https://*.example.com", origin: "https://api.sub.example.com", want: true},
		{pattern: "*.example.com", origin: "https://example.org", want: false},
		{pattern: "*.example.com", origin: "://invalid-url", want: false},
	}

	for _, tt := range tests {
		got := originMatches(tt.pattern, tt.origin)
		if got != tt.want {
			t.Errorf("originMatches(%q, %q) = %v, want %v", tt.pattern, tt.origin, got, tt.want)
		}
	}
}

func TestWithHeaders(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	t.Run("OPTIONS preflight returns 204", func(t *testing.T) {
		t.Setenv("FRONTEND_ORIGINS", "https://example.com")
		handler := withHeaders(dummyHandler)

		req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status %d for OPTIONS, got %d", http.StatusNoContent, rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Errorf("expected Access-Control-Allow-Origin to be https://example.com, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("expected Access-Control-Allow-Credentials true, got %q", rec.Header().Get("Access-Control-Allow-Credentials"))
		}
	})

	t.Run("No Origin header defaults to wildcard", func(t *testing.T) {
		t.Setenv("FRONTEND_ORIGINS", "https://example.com")
		handler := withHeaders(dummyHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected wildcard origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("expected nosniff header, got %q", rec.Header().Get("X-Content-Type-Options"))
		}
	})

	t.Run("Disallowed Origin header is not reflected", func(t *testing.T) {
		t.Setenv("FRONTEND_ORIGINS", "https://trusted.com")
		handler := withHeaders(dummyHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("Origin", "https://malicious.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") == "https://malicious.com" {
			t.Errorf("disallowed origin should not be accepted")
		}
	})
}

func TestEnvOr(t *testing.T) {
	t.Setenv("PORTFOLIO_TEST_KEY", "custom_val")
	if got := envOr("PORTFOLIO_TEST_KEY", "fallback"); got != "custom_val" {
		t.Errorf("envOr() = %q, want custom_val", got)
	}

	t.Setenv("PORTFOLIO_TEST_KEY", "")
	if got := envOr("PORTFOLIO_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("envOr() = %q, want fallback", got)
	}
}

func TestLoadEnvFiles(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env.test")
	content := `# Comment line
TEST_ENV_VAR_1=value1
TEST_ENV_VAR_2="quoted value"
TEST_ENV_VAR_3='single quoted'

`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("write temp env: %v", err)
	}

	loadEnvFiles(envPath)
	if os.Getenv("TEST_ENV_VAR_1") != "value1" {
		t.Errorf("TEST_ENV_VAR_1 = %q, want value1", os.Getenv("TEST_ENV_VAR_1"))
	}
	if os.Getenv("TEST_ENV_VAR_2") != "quoted value" {
		t.Errorf("TEST_ENV_VAR_2 = %q, want 'quoted value'", os.Getenv("TEST_ENV_VAR_2"))
	}
	if os.Getenv("TEST_ENV_VAR_3") != "single quoted" {
		t.Errorf("TEST_ENV_VAR_3 = %q, want 'single quoted'", os.Getenv("TEST_ENV_VAR_3"))
	}
}
