package main

import (
	"net/http"
	"testing"
)

func TestAllowWebSocketOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		env    string
		want   bool
	}{
		{name: "same origin", host: "shellwave.local:4000", origin: "http://shellwave.local:4000", want: true},
		{name: "vite dev", host: "localhost:4000", origin: "http://localhost:5173", want: true},
		{name: "configured", host: "shellwave.local:4000", origin: "https://ops.example.com", env: "https://ops.example.com", want: true},
		{name: "external", host: "shellwave.local:4000", origin: "https://evil.example.com", want: false},
		{name: "bad origin", host: "shellwave.local:4000", origin: "://bad", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELLWAVE_ALLOWED_ORIGINS", tt.env)
			req := &http.Request{Host: tt.host, Header: http.Header{}}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if got := allowWebSocketOrigin(req); got != tt.want {
				t.Fatalf("allowWebSocketOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTLSFiles(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		t.Setenv("SHELLWAVE_TLS_CERT", "")
		t.Setenv("SHELLWAVE_TLS_KEY", "")
		cert, key, err := resolveTLSFiles("", "")
		if err != nil {
			t.Fatalf("resolve tls files: %v", err)
		}
		if cert != "" || key != "" {
			t.Fatalf("expected TLS disabled, got cert=%q key=%q", cert, key)
		}
	})

	t.Run("requires pair", func(t *testing.T) {
		t.Setenv("SHELLWAVE_TLS_CERT", "")
		t.Setenv("SHELLWAVE_TLS_KEY", "")
		if _, _, err := resolveTLSFiles("cert.pem", ""); err == nil {
			t.Fatal("expected missing key error")
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("SHELLWAVE_TLS_CERT", "env-cert.pem")
		t.Setenv("SHELLWAVE_TLS_KEY", "env-key.pem")
		cert, key, err := resolveTLSFiles("", "")
		if err != nil {
			t.Fatalf("resolve tls files: %v", err)
		}
		if cert != "env-cert.pem" || key != "env-key.pem" {
			t.Fatalf("unexpected env tls files cert=%q key=%q", cert, key)
		}
	})
}
