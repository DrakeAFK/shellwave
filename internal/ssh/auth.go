package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	sshlib "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

type AuthConfig struct {
	Type       string `json:"type,omitempty"`
	Password   string `json:"password,omitempty"`
	KeyPath    string `json:"keyPath,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	UseAgent   bool   `json:"useAgent,omitempty"`
}

func ResolveAuthMethods(auth AuthConfig) ([]sshlib.AuthMethod, error) {
	authType := strings.ToLower(strings.TrimSpace(auth.Type))
	if authType == "" {
		switch {
		case auth.UseAgent:
			authType = "agent"
		case strings.TrimSpace(auth.KeyPath) != "":
			authType = "key"
		case auth.Password != "":
			authType = "password"
		default:
			authType = "agent"
		}
	}

	switch authType {
	case "password":
		if auth.Password == "" {
			return nil, errors.New("password auth selected but no password was provided")
		}
		return []sshlib.AuthMethod{sshlib.Password(auth.Password)}, nil
	case "key":
		return keyAuth(auth.KeyPath, auth.Passphrase)
	case "agent":
		return agentAuth()
	default:
		return nil, fmt.Errorf("unsupported SSH auth mode %q", auth.Type)
	}
}

func keyAuth(path, passphrase string) ([]sshlib.AuthMethod, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("key auth selected but no key path was configured")
	}
	key, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("read SSH key: %w", err)
	}
	var signer sshlib.Signer
	if passphrase != "" {
		signer, err = sshlib.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	} else {
		signer, err = sshlib.ParsePrivateKey(key)
	}
	if err != nil {
		return nil, fmt.Errorf("parse SSH key: %w", err)
	}
	return []sshlib.AuthMethod{sshlib.PublicKeys(signer)}, nil
}

func agentAuth() ([]sshlib.AuthMethod, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, errors.New("SSH agent auth selected but SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("connect to SSH agent: %w", err)
	}
	client := sshagent.NewClient(conn)
	return []sshlib.AuthMethod{sshlib.PublicKeysCallback(client.Signers)}, nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}
