package ssh

import (
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

type Session struct {
	Client  *ssh.Client
	Session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
}

type SessionConfig struct {
	User            string
	Host            string
	Port            int
	Auth            AuthConfig
	HostKeyCallback ssh.HostKeyCallback
	Cols            int
	Rows            int
	Timeout         time.Duration
}

func NewSession(user, host string, port int, auth ssh.AuthMethod) (*Session, error) {
	return NewSessionWithSize(user, host, port, auth, 80, 24)
}

func NewSessionWithSize(user, host string, port int, auth ssh.AuthMethod, cols, rows int) (*Session, error) {
	return NewSessionWithOptions(user, host, port, auth, cols, rows, 10*time.Second)
}

func NewSessionWithOptions(user, host string, port int, auth ssh.AuthMethod, cols, rows int, timeout time.Duration) (*Session, error) {
	return NewSessionWithAuthMethods(user, host, port, []ssh.AuthMethod{auth}, cols, rows, timeout)
}

func NewSessionWithAuthMethods(user, host string, port int, auth []ssh.AuthMethod, cols, rows int, timeout time.Duration) (*Session, error) {
	return newSession(user, host, port, auth, nil, cols, rows, timeout)
}

func NewSessionWithConfig(cfg SessionConfig) (*Session, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	authMethods, err := ResolveAuthMethods(cfg.Auth)
	if err != nil {
		return nil, err
	}
	return newSession(cfg.User, cfg.Host, cfg.Port, authMethods, cfg.HostKeyCallback, cfg.Cols, cfg.Rows, cfg.Timeout)
}

func newSession(user, host string, port int, auth []ssh.AuthMethod, hostKeyCallback ssh.HostKeyCallback, cols, rows int, timeout time.Duration) (*Session, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if hostKeyCallback == nil {
		return nil, errors.New("SSH host key callback is required")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	return &Session{
		Client:  client,
		Session: session,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
	}, nil
}

func (s *Session) Resize(cols, rows int) error {
	if s.Session == nil {
		return nil
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return s.Session.WindowChange(rows, cols)
}

func (s *Session) Wait() error {
	if s.Session == nil {
		return nil
	}
	return s.Session.Wait()
}

func (s *Session) Close() {
	if s.Session != nil {
		s.Session.Close()
	}
	if s.Client != nil {
		s.Client.Close()
	}
}
