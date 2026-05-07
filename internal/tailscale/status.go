package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"shellwave/internal/devices"
)

type Status struct {
	Available bool             `json:"available"`
	Message   string           `json:"message,omitempty"`
	Self      *Peer            `json:"self,omitempty"`
	Peers     []Peer           `json:"peers"`
	Devices   []devices.Device `json:"devices"`
}

type Peer struct {
	ID          string    `json:"id,omitempty"`
	HostName    string    `json:"hostName,omitempty"`
	DNSName     string    `json:"dnsName,omitempty"`
	TailscaleIP string    `json:"tailscaleIp,omitempty"`
	OS          string    `json:"os,omitempty"`
	Online      bool      `json:"online"`
	LastSeen    time.Time `json:"lastSeen,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
}

type rawStatus struct {
	Self rawPeer            `json:"Self"`
	Peer map[string]rawPeer `json:"Peer"`
}

type rawPeer struct {
	ID           string    `json:"ID"`
	HostName     string    `json:"HostName"`
	DNSName      string    `json:"DNSName"`
	TailscaleIPs []string  `json:"TailscaleIPs"`
	OS           string    `json:"OS"`
	Online       bool      `json:"Online"`
	LastSeen     time.Time `json:"LastSeen"`
	Tags         []string  `json:"Tags"`
}

func LocalStatus(ctx context.Context) (Status, error) {
	bin, err := findTailscale()
	if err != nil {
		return Status{Available: false, Message: err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				msg = "tailscale status failed"
			}
			return Status{Available: false, Message: msg}, nil
		}
		return Status{Available: false, Message: err.Error()}, nil
	}
	status, err := ParseStatus(out)
	if err != nil {
		return Status{}, err
	}
	return status, nil
}

func findTailscale() (string, error) {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path, nil
	}
	candidates := []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("tailscale CLI is not installed or not on PATH")
}

func ParseStatus(data []byte) (Status, error) {
	var raw rawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, fmt.Errorf("parse tailscale status: %w", err)
	}

	self := toPeer(raw.Self)
	status := Status{Available: true, Self: &self}
	if self.ID != "" || self.TailscaleIP != "" || self.DNSName != "" {
		status.Devices = append(status.Devices, toDevice(self))
	}
	for _, peer := range raw.Peer {
		p := toPeer(peer)
		status.Peers = append(status.Peers, p)
		status.Devices = append(status.Devices, toDevice(p))
	}
	return status, nil
}

func toPeer(raw rawPeer) Peer {
	ip := ""
	if len(raw.TailscaleIPs) > 0 {
		ip = raw.TailscaleIPs[0]
	}
	return Peer{
		ID:          raw.ID,
		HostName:    raw.HostName,
		DNSName:     strings.TrimSuffix(raw.DNSName, "."),
		TailscaleIP: ip,
		OS:          raw.OS,
		Online:      raw.Online,
		LastSeen:    raw.LastSeen,
		Tags:        raw.Tags,
	}
}

func toDevice(peer Peer) devices.Device {
	host := peer.TailscaleIP
	if host == "" {
		host = peer.DNSName
	}
	name := peer.HostName
	if name == "" {
		name = host
	}
	return devices.Normalize(devices.Device{
		ID:          tailscaleDeviceID(peer),
		Name:        name,
		Host:        host,
		TailscaleIP: peer.TailscaleIP,
		MagicDNS:    peer.DNSName,
		User:        "root",
		Port:        22,
		AuthMode:    "password",
		Source:      "tailscale",
		Online:      peer.Online,
		LastSeen:    peer.LastSeen,
		Tags:        peer.Tags,
		OS:          peer.OS,
	})
}

func tailscaleDeviceID(peer Peer) string {
	if peer.ID != "" {
		return devices.NewID(peer.ID)
	}
	return devices.NewID(peer.DNSName, peer.TailscaleIP)
}
