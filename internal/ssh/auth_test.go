package ssh

import "testing"

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

func TestResolveUnsupportedAuthMode(t *testing.T) {
	_, err := ResolveAuthMethods(AuthConfig{Type: "agent"})
	if err == nil {
		t.Fatal("expected unsupported auth mode error")
	}
}
