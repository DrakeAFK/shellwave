package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	sshlib "golang.org/x/crypto/ssh"
)

type RunResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

type RunConfig struct {
	User            string
	Host            string
	Port            int
	Auth            AuthConfig
	HostKeyCallback sshlib.HostKeyCallback
	Command         string
	Timeout         time.Duration
}

func Run(ctx context.Context, cfg RunConfig) (RunResult, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}

	return run(ctx, cfg)
}

func run(ctx context.Context, cfg RunConfig) (RunResult, error) {
	if cfg.HostKeyCallback == nil {
		return RunResult{ExitCode: -1}, errors.New("SSH host key callback is required")
	}
	authMethods, err := ResolveAuthMethods(cfg.Auth)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	clientConfig := &sshlib.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: cfg.HostKeyCallback,
		Timeout:         cfg.Timeout,
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	dialer := net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}

	sshConn, chans, reqs, err := sshlib.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		_ = conn.Close()
		return RunResult{ExitCode: -1}, err
	}
	client := sshlib.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	runCh := make(chan error, 1)
	go func() {
		runCh <- session.Run(cfg.Command)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		_ = session.Close()
		_ = client.Close()
		return RunResult{ExitCode: -1}, ctx.Err()
	case runErr = <-runCh:
	}
	result := RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		var exitErr *sshlib.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			return result, nil
		}
		result.ExitCode = -1
		return result, runErr
	}
	return result, nil
}
