package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePasswordAuth(t *testing.T) {
	methods, err := ResolveAuthMethods(AuthConfig{Type: "password", Password: "secret"})
	if err != nil {
		t.Fatalf("resolve password auth: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected one auth method, got %d", len(methods))
	}
}

func TestResolvePasswordAuthRequiresPassword(t *testing.T) {
	_, err := ResolveAuthMethods(AuthConfig{Type: "password"})
	if err == nil {
		t.Fatal("expected missing password error")
	}
}

func TestResolveKeyAuth(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	methods, err := ResolveAuthMethods(AuthConfig{Type: "key", KeyPath: keyPath})
	if err != nil {
		t.Fatalf("resolve key auth: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected one auth method, got %d", len(methods))
	}
}

func TestResolveAgentAuthRequiresSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := ResolveAuthMethods(AuthConfig{Type: "agent"})
	if err == nil {
		t.Fatal("expected missing SSH_AUTH_SOCK error")
	}
}
