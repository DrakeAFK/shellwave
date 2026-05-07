package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shellwave/internal/devices"
)

func TestStorePersistsDevicesWithoutPasswordField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shellwave.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	saved, err := s.UpsertDevice(devices.Device{Name: "node", Host: "100.64.0.2", User: "root"})
	if err != nil {
		t.Fatalf("save device: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	got, ok := reopened.GetDevice(saved.ID)
	if !ok {
		t.Fatal("expected device after reopen")
	}
	if got.Host != "100.64.0.2" || got.Port != 22 {
		t.Fatalf("unexpected device: %#v", got)
	}
}

func TestStoreUpdatesAndDeletesDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shellwave.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	saved, err := s.UpsertDevice(devices.Device{Name: "node", Host: "100.64.0.2", User: "root"})
	if err != nil {
		t.Fatalf("save device: %v", err)
	}
	saved.Name = "renamed"
	saved.User = "drake"
	updated, err := s.UpsertDevice(saved)
	if err != nil {
		t.Fatalf("update device: %v", err)
	}
	if updated.Name != "renamed" || updated.User != "drake" {
		t.Fatalf("unexpected updated device: %#v", updated)
	}
	if err := s.DeleteDevice(saved.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	if _, ok := s.GetDevice(saved.ID); ok {
		t.Fatal("expected device to be deleted")
	}
}

func TestStoreMigratesLegacyJSONToSQLite(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "shellwave.json")
	if err := os.WriteFile(jsonPath, []byte(`{"devices":[{"id":"node-1","name":"node","host":"100.64.0.2","user":"root","port":22,"source":"manual","createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-01T00:00:00Z"}]}`), 0o600); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}

	s, err := Open(jsonPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer s.Close()

	if !strings.HasSuffix(s.Path(), "shellwave.db") {
		t.Fatalf("expected sqlite migration target, got %s", s.Path())
	}
	got, ok := s.GetDevice("node-1")
	if !ok {
		t.Fatal("expected migrated device")
	}
	if got.Host != "100.64.0.2" || got.AuthMode != "password" {
		t.Fatalf("unexpected migrated device: %#v", got)
	}
}

func TestMergeDevicesMatchesExistingTailnetDeviceByIP(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	existing, err := s.UpsertDevice(devices.Device{
		ID:          "old-node-id",
		Name:        "server",
		Host:        "100.64.0.20",
		TailscaleIP: "100.64.0.20",
		MagicDNS:    "server.tail.ts.net",
		User:        "drake",
		Port:        2222,
		AuthMode:    "password",
		Source:      "tailscale",
		Favorite:    true,
		Notes:       "keep me",
	})
	if err != nil {
		t.Fatalf("seed device: %v", err)
	}

	if err := s.MergeDevices([]devices.Device{{
		ID:          "new-node-id",
		Name:        "server-renamed",
		Host:        "100.64.0.20",
		TailscaleIP: "100.64.0.20",
		MagicDNS:    "server-new.tail.ts.net",
		User:        "root",
		Port:        22,
		AuthMode:    "password",
		Source:      "tailscale",
		Online:      true,
		OS:          "linux",
	}}); err != nil {
		t.Fatalf("merge devices: %v", err)
	}

	items, err := s.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one merged device, got %d: %#v", len(items), items)
	}
	got := items[0]
	if got.ID != existing.ID || got.User != "drake" || got.Port != 2222 {
		t.Fatalf("expected existing connection settings to be preserved, got %#v", got)
	}
	if got.Name != "server-renamed" || got.MagicDNS != "server-new.tail.ts.net" || !got.Favorite || got.Notes != "keep me" || !got.Online {
		t.Fatalf("expected imported metadata and local preferences to merge, got %#v", got)
	}
}

func TestListDevicesDedupesOldAndNewTailnetRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	oldRow, err := s.UpsertDevice(devices.Device{
		ID:          "old-format-id",
		Name:        "server-old",
		Host:        "server.tail.ts.net",
		TailscaleIP: "100.64.0.20",
		MagicDNS:    "server.tail.ts.net",
		User:        "ubuntu",
		Port:        2222,
		AuthMode:    "password",
		Source:      "tailscale",
		Favorite:    true,
		Notes:       "preserve local note",
		OS:          "linux",
	})
	if err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	newRow, err := s.UpsertDevice(devices.Device{
		ID:          "new-format-id",
		Name:        "server-new",
		Host:        "100.64.0.20",
		TailscaleIP: "100.64.0.20",
		MagicDNS:    "server.tail.ts.net",
		User:        "root",
		Port:        22,
		AuthMode:    "password",
		Source:      "tailscale",
		Online:      true,
		OS:          "linux",
	})
	if err != nil {
		t.Fatalf("seed new row: %v", err)
	}

	items, err := s.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected duplicate tailnet rows to be collapsed, got %d: %#v", len(items), items)
	}
	got := items[0]
	if got.ID != newRow.ID {
		t.Fatalf("expected IP-host row to be canonical id %q, got %q; old row was %q", newRow.ID, got.ID, oldRow.ID)
	}
	if got.Host != "100.64.0.20" || got.User != "ubuntu" || got.Port != 2222 || !got.Favorite || got.Notes != "preserve local note" || !got.Online {
		t.Fatalf("unexpected deduped device: %#v", got)
	}
	if _, ok := s.GetDevice(oldRow.ID); ok {
		t.Fatal("expected old duplicate row to be deleted")
	}
}

func TestKnownHostCRUD(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	trusted, err := s.TrustKnownHost(KnownHost{
		Host:              "server.tail.ts.net",
		Port:              22,
		KeyType:           "ssh-ed25519",
		FingerprintSHA256: "SHA256:abc",
		PublicKey:         "ssh-ed25519 AAAA",
	})
	if err != nil {
		t.Fatalf("trust host: %v", err)
	}
	got, ok, err := s.FindKnownHost("server.tail.ts.net", 22)
	if err != nil {
		t.Fatalf("find known host: %v", err)
	}
	if !ok || got.FingerprintSHA256 != "SHA256:abc" {
		t.Fatalf("unexpected known host: %#v", got)
	}
	if err := s.DeleteKnownHost(trusted.ID); err != nil {
		t.Fatalf("delete known host: %v", err)
	}
	if _, ok, err := s.FindKnownHost("server.tail.ts.net", 22); err != nil || ok {
		t.Fatalf("expected known host to be deleted, ok=%v err=%v", ok, err)
	}
}
