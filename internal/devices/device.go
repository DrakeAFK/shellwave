package devices

import "time"

type Device struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	TailscaleIP string    `json:"tailscaleIp,omitempty"`
	MagicDNS    string    `json:"magicDns,omitempty"`
	User        string    `json:"user"`
	Port        int       `json:"port"`
	AuthMode    string    `json:"authMode"`
	KeyPath     string    `json:"keyPath,omitempty"`
	Source      string    `json:"source"`
	Online      bool      `json:"online"`
	LastSeen    time.Time `json:"lastSeen,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	OS          string    `json:"os,omitempty"`
	Favorite    bool      `json:"favorite,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (d Device) SSHHost() string {
	if d.Host != "" {
		return d.Host
	}
	if d.MagicDNS != "" {
		return d.MagicDNS
	}
	return d.TailscaleIP
}

func Normalize(d Device) Device {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = NewID(d.Name, d.Host, d.MagicDNS, d.TailscaleIP)
	}
	if d.Name == "" {
		d.Name = d.Host
	}
	if d.Port == 0 {
		d.Port = 22
	}
	if d.User == "" {
		d.User = "root"
	}
	if d.Source == "" {
		d.Source = "manual"
	}
	if d.AuthMode == "" {
		d.AuthMode = "password"
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	return d
}
