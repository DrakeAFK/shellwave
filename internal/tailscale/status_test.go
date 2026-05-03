package tailscale

import (
	"testing"

	"shellwave/internal/devices"
)

func TestParseStatus(t *testing.T) {
	data := []byte(`{
		"Self": {"ID":"self","HostName":"workstation","DNSName":"work.tail.ts.net.","TailscaleIPs":["100.64.0.10"],"OS":"macOS","Online":true},
		"Peer": {
			"peer1": {"ID":"peer1","HostName":"server","DNSName":"server.tail.ts.net.","TailscaleIPs":["100.64.0.20"],"OS":"linux","Online":true,"Tags":["tag:prod"]}
		}
	}`)
	status, err := ParseStatus(data)
	if err != nil {
		t.Fatalf("parse status: %v", err)
	}
	if !status.Available {
		t.Fatal("expected available status")
	}
	if status.Self == nil || status.Self.HostName != "workstation" {
		t.Fatalf("expected self peer, got %#v", status.Self)
	}
	if len(status.Devices) != 2 {
		t.Fatalf("expected self plus one peer device, got %d", len(status.Devices))
	}
	self := status.Devices[0]
	if self.Name != "workstation" || self.Host != defaultSelfSSHHost || self.MagicDNS != "work.tail.ts.net" || self.TailscaleIP != "100.64.0.10" {
		t.Fatalf("unexpected self device: %#v", self)
	}
	device := status.Devices[1]
	if device.Name != "server" || device.Host != "server.tail.ts.net" || device.TailscaleIP != "100.64.0.20" {
		t.Fatalf("unexpected device: %#v", device)
	}
	if device.Source != "tailscale" || !device.Online || device.OS != "linux" {
		t.Fatalf("unexpected metadata: %#v", device)
	}
	if device.AuthMode != "password" {
		t.Fatalf("expected imported auth mode to default to password, got %q", device.AuthMode)
	}
}

func TestApplySelfSSHHost(t *testing.T) {
	status := Status{
		Self: &Peer{ID: "self", DNSName: "work.tail.ts.net", TailscaleIP: "100.64.0.10"},
		Devices: []devices.Device{
			toSelfDevice(Peer{ID: "self", HostName: "workstation", DNSName: "work.tail.ts.net", TailscaleIP: "100.64.0.10"}, defaultSelfSSHHost),
			toDevice(Peer{ID: "peer1", HostName: "server", DNSName: "server.tail.ts.net", TailscaleIP: "100.64.0.20"}),
		},
	}

	applySelfSSHHost(&status, "host.docker.internal")

	if status.Devices[0].Host != "host.docker.internal" {
		t.Fatalf("expected self host override, got %#v", status.Devices[0])
	}
	if status.Devices[1].Host != "server.tail.ts.net" {
		t.Fatalf("peer host should not be changed: %#v", status.Devices[1])
	}
}
