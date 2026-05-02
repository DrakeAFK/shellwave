package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"

	"shellwave/internal/store"

	sshlib "golang.org/x/crypto/ssh"
)

func TestHostKeyCallbackTrustFlow(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	key := testPublicKey(t)
	callback := HostKeyCallback(s, "server.tail.ts.net", 22)
	err = callback("server.tail.ts.net", nil, key)
	var hostKeyErr *HostKeyError
	if err == nil {
		t.Fatal("expected unknown host error")
	}
	if !asHostKeyError(err, &hostKeyErr) || hostKeyErr.Kind != "unknown_host" {
		t.Fatalf("unexpected error: %#v", err)
	}

	if _, err := s.TrustKnownHost(store.KnownHost{
		Host:              "server.tail.ts.net",
		Port:              22,
		KeyType:           key.Type(),
		FingerprintSHA256: sshlib.FingerprintSHA256(key),
		PublicKey:         string(sshlib.MarshalAuthorizedKey(key)),
	}); err != nil {
		t.Fatalf("trust host: %v", err)
	}
	if err := callback("server.tail.ts.net", nil, key); err != nil {
		t.Fatalf("expected trusted host to pass: %v", err)
	}

	changedKey := testPublicKey(t)
	err = callback("server.tail.ts.net", nil, changedKey)
	if !asHostKeyError(err, &hostKeyErr) || hostKeyErr.Kind != "host_key_changed" {
		t.Fatalf("expected changed host key error, got %#v", err)
	}
}

func asHostKeyError(err error, target **HostKeyError) bool {
	if err == nil {
		return false
	}
	if typed, ok := err.(*HostKeyError); ok {
		*target = typed
		return true
	}
	return false
}

func testPublicKey(t *testing.T) sshlib.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := sshlib.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	return key
}
