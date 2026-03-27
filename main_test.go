package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSanitizePathNormalizesUnsafeInput(t *testing.T) {
	t.Parallel()

	got := sanitizePath("/api/logs/../../metrics?foo=bar")
	if got != "/metrics" {
		t.Fatalf("sanitizePath() = %q, want %q", got, "/metrics")
	}
}

func TestClientIPRejectsInvalidForwardedChain(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Forwarded-For", "bad-ip, 10.0.0.2")

	if got := clientIP(req); got != "unknown" {
		t.Fatalf("clientIP() = %q, want %q", got, "unknown")
	}
}

func TestNewServerSetsSecurityTimeouts(t *testing.T) {
	t.Parallel()

	server := newServer(http.NewServeMux())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, 5*time.Second)
	}
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, 10*time.Second)
	}
	if server.WriteTimeout != 15*time.Second {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, 15*time.Second)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, 60*time.Second)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, 1<<20)
	}
}
