package ssh

import (
	"fmt"
	"net"
	"strings"

	"shellwave/internal/store"

	sshlib "golang.org/x/crypto/ssh"
)

type HostKeyDetails struct {
	Host                    string `json:"host"`
	Port                    int    `json:"port"`
	KeyType                 string `json:"keyType"`
	FingerprintSHA256       string `json:"fingerprintSha256"`
	PublicKey               string `json:"publicKey"`
	KnownFingerprintSHA256  string `json:"knownFingerprintSha256,omitempty"`
	KnownPublicKeyAvailable bool   `json:"knownPublicKeyAvailable,omitempty"`
}

type HostKeyError struct {
	Kind    string         `json:"kind"`
	Details HostKeyDetails `json:"details"`
}

func (e *HostKeyError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case "unknown_host":
		return fmt.Sprintf("unknown SSH host key for %s:%d (%s)", e.Details.Host, e.Details.Port, e.Details.FingerprintSHA256)
	case "host_key_changed":
		return fmt.Sprintf("SSH host key changed for %s:%d", e.Details.Host, e.Details.Port)
	default:
		return "SSH host key verification failed"
	}
}

func HostKeyCallback(knownHosts *store.Store, host string, port int) sshlib.HostKeyCallback {
	if port == 0 {
		port = 22
	}
	return func(_ string, _ net.Addr, key sshlib.PublicKey) error {
		fingerprint := sshlib.FingerprintSHA256(key)
		publicKey := strings.TrimSpace(string(sshlib.MarshalAuthorizedKey(key)))
		details := HostKeyDetails{
			Host:              host,
			Port:              port,
			KeyType:           key.Type(),
			FingerprintSHA256: fingerprint,
			PublicKey:         publicKey,
		}
		record, ok, err := knownHosts.FindKnownHost(host, port)
		if err != nil {
			return err
		}
		if !ok {
			return &HostKeyError{Kind: "unknown_host", Details: details}
		}
		if record.FingerprintSHA256 != fingerprint {
			details.KnownFingerprintSHA256 = record.FingerprintSHA256
			details.KnownPublicKeyAvailable = record.PublicKey != ""
			return &HostKeyError{Kind: "host_key_changed", Details: details}
		}
		return nil
	}
}
