package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	resultCh := make(chan runOutcome, 1)
	go func() {
		result, err := run(cfg)
		resultCh <- runOutcome{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1}, ctx.Err()
	case outcome := <-resultCh:
		return outcome.result, outcome.err
	}
}

func RunPassword(ctx context.Context, user, host string, port int, password, command string, timeout time.Duration) (RunResult, error) {
	return Run(ctx, RunConfig{
		User:    user,
		Host:    host,
		Port:    port,
		Auth:    AuthConfig{Type: "password", Password: password},
		Command: command,
		Timeout: timeout,
	})
}

type runOutcome struct {
	result RunResult
	err    error
}

func run(cfg RunConfig) (RunResult, error) {
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
	client, err := sshlib.Dial("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), clientConfig)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
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

	err = session.Run(cfg.Command)
	result := RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *sshlib.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			return result, nil
		}
		result.ExitCode = -1
		return result, err
	}
	return result, nil
}
