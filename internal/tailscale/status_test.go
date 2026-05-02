package tailscale

import "testing"

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
	if len(status.Devices) != 1 {
		t.Fatalf("expected one device, got %d", len(status.Devices))
	}
	device := status.Devices[0]
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
