package ssh

import (
	"errors"
	"fmt"
	"strings"

	sshlib "golang.org/x/crypto/ssh"
)

type AuthConfig struct {
	Type     string `json:"type,omitempty"`
	Password string `json:"password,omitempty"`
}

func ResolveAuthMethods(auth AuthConfig) ([]sshlib.AuthMethod, error) {
	authType := strings.ToLower(strings.TrimSpace(auth.Type))
	if authType == "" {
		authType = "password"
	}

	if authType != "password" {
		return nil, fmt.Errorf("unsupported SSH auth mode %q: ShellWave currently supports password auth only", auth.Type)
	}
	if auth.Password == "" {
		return nil, errors.New("password auth selected but no password was provided")
	}
	return []sshlib.AuthMethod{sshlib.Password(auth.Password)}, nil
}
