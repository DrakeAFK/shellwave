package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shellwave/internal/httpapi"
	"shellwave/internal/ssh"
	"shellwave/internal/store"
	wsproto "shellwave/internal/ws"

	"github.com/gorilla/websocket"
	sshlib "golang.org/x/crypto/ssh"
)

var (
	addr       = flag.String("addr", "127.0.0.1:4000", "Address to listen on")
	staticPath = flag.String("static", "./web/dist", "Path to static files")
	dataPath   = flag.String("data", envDefault("SHELLWAVE_DATA", ""), "Path to ShellWave data file. Can also be set with SHELLWAVE_DATA")
	tlsCert    = flag.String("tls-cert", "", "Path to TLS certificate file. Can also be set with SHELLWAVE_TLS_CERT")
	tlsKey     = flag.String("tls-key", "", "Path to TLS private key file. Can also be set with SHELLWAVE_TLS_KEY")
)

var upgrader = websocket.Upgrader{
	CheckOrigin: allowWebSocketOrigin,
}

func envDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value != "" {
		return value
	}
	return fallback
}

func handleWS(appStore *store.Store, api *httpapi.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !api.IsAuthenticated(r) {
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade: %v", err)
			return
		}
		writer := newSafeWSWriter(conn)
		defer writer.close()

		connect, err := readClientMessage(conn)
		if err != nil {
			writer.writeJSON(wsproto.Error("invalid connect message: " + err.Error()))
			return
		}
		if err := wsproto.ValidateConnect(connect); err != nil {
			writer.writeJSON(wsproto.Error(err.Error()))
			return
		}
		if connect.DeviceID == "" {
			writer.writeJSON(wsproto.Error("device id is required"))
			return
		}
		device, ok := appStore.GetDevice(connect.DeviceID)
		if !ok {
			writer.writeJSON(wsproto.Error("device not found"))
			return
		}
		connect.Host = device.SSHHost()
		connect.User = device.User
		connect.Port = device.Port
		connect.Auth.Type = "password"
		port := connect.Port
		if port == 0 {
			port = 22
		}
		if !api.AllowSSHAttempt(r) {
			writer.writeJSON(wsproto.Error("Too many SSH connection attempts. Try again later."))
			return
		}
		if err := api.CheckHostAllowed(r.Context(), connect.Host); err != nil {
			writer.writeJSON(wsproto.Error(err.Error()))
			return
		}

		log.Printf("terminal connect host=%s user=%s port=%d", connect.Host, connect.User, port)
		writer.writeJSON(wsproto.Status(wsproto.StateConnecting))

		session, err := ssh.NewSessionWithConfig(ssh.SessionConfig{
			User:            connect.User,
			Host:            connect.Host,
			Port:            port,
			Auth:            ssh.AuthConfig(connect.Auth),
			HostKeyCallback: ssh.HostKeyCallback(appStore, connect.Host, port),
			Cols:            connect.Cols,
			Rows:            connect.Rows,
			Timeout:         15 * time.Second,
		})
		if err != nil {
			writer.writeJSON(wsproto.Status(wsproto.StateError))
			var hostKeyErr *ssh.HostKeyError
			if errors.As(err, &hostKeyErr) {
				writer.writeJSON(wsproto.HostKeyError(hostKeyErr.Kind, hostKeyErr.Error(), toWSHostKey(hostKeyErr.Details)))
			} else {
				writer.writeJSON(wsproto.ErrorWithCode("ssh_failed", "SSH connection failed: "+friendlySSHError(err)))
			}
			return
		}
		defer session.Close()
		writer.writeJSON(wsproto.Status(wsproto.StateConnected))
		if connect.Cols > 0 && connect.Rows > 0 {
			_ = session.Resize(connect.Cols, connect.Rows)
		}

		go func() {
			_, _ = io.Copy(&terminalOutputWriter{writer: writer}, session.Stdout)
		}()
		go func() {
			_, _ = io.Copy(&terminalOutputWriter{writer: writer}, session.Stderr)
		}()
		go func() {
			code := 0
			if err := session.Wait(); err != nil {
				var exitErr *sshlib.ExitError
				if errors.As(err, &exitErr) {
					code = exitErr.ExitStatus()
				} else {
					code = 1
				}
			}
			writer.writeJSON(wsproto.Exit(code))
			writer.writeJSON(wsproto.Status(wsproto.StateDisconnected))
			writer.close()
		}()

		for {
			msg, err := readClientMessage(conn)
			if err != nil {
				session.Close()
				return
			}
			switch msg.Type {
			case wsproto.ClientTypeInput:
				_, _ = session.Stdin.Write([]byte(msg.Data))
			case wsproto.ClientTypeResize:
				if err := session.Resize(msg.Cols, msg.Rows); err != nil {
					writer.writeJSON(wsproto.Error("resize failed: " + err.Error()))
				}
			case wsproto.ClientTypePing:
				writer.writeJSON(wsproto.Status(wsproto.StateConnected))
			default:
				writer.writeJSON(wsproto.Error("unsupported message type: " + msg.Type))
			}
		}
	}
}

func readClientMessage(conn *websocket.Conn) (wsproto.ClientMessage, error) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return wsproto.ClientMessage{}, err
	}
	return wsproto.DecodeClientMessage(data)
}

type safeWSWriter struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

func newSafeWSWriter(conn *websocket.Conn) *safeWSWriter {
	return &safeWSWriter{conn: conn}
}

func (w *safeWSWriter) writeJSON(msg wsproto.ServerMessage) error {
	data, err := wsproto.EncodeServerMessage(msg)
	if err != nil {
		return err
	}
	return w.write(websocket.TextMessage, data)
}

func (w *safeWSWriter) write(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return websocket.ErrCloseSent
	}
	if err := w.conn.WriteMessage(messageType, data); err != nil {
		w.closed = true
		return err
	}
	return nil
}

func (w *safeWSWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	_ = w.conn.Close()
}

type terminalOutputWriter struct {
	writer *safeWSWriter
}

func (w *terminalOutputWriter) Write(p []byte) (int, error) {
	if err := w.writer.writeJSON(wsproto.Output(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func friendlySSHError(err error) string {
	message := err.Error()
	if strings.Contains(message, "unable to authenticate") {
		return "authentication failed. Check the session password, SSH user, and whether password login is enabled on the server."
	}
	if strings.Contains(message, "connection refused") {
		return "connection refused. Check the host, port, and firewall."
	}
	if strings.Contains(message, "i/o timeout") || strings.Contains(message, "no route to host") {
		return "host is unreachable. Check the address, Tailscale connectivity, and firewall."
	}
	return message
}

func allowWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return isLocalRequest(r)
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	normalized := strings.TrimRight(origin, "/")
	if isDevMode() || isLocalHostPort(r.Host) {
		if _, ok := defaultDevOrigins()[normalized]; ok {
			return true
		}
	}
	if _, ok := configuredOrigins()[normalized]; ok {
		return true
	}
	return false
}

func isDevMode() bool {
	return strings.EqualFold(os.Getenv("SHELLWAVE_DEV"), "true")
}

func isLocalHostPort(hostPort string) bool {
	host := hostPort
	if parsedHost, _, err := net.SplitHostPort(hostPort); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultDevOrigins() map[string]struct{} {
	return map[string]struct{}{
		"http://localhost:5173": {},
		"http://127.0.0.1:5173": {},
	}
}

func configuredOrigins() map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, raw := range strings.Split(os.Getenv("SHELLWAVE_ALLOWED_ORIGINS"), ",") {
		origin := strings.TrimRight(strings.TrimSpace(raw), "/")
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return allowed
}

func resolveTLSFiles(certFlag, keyFlag string) (string, string, error) {
	cert := strings.TrimSpace(certFlag)
	key := strings.TrimSpace(keyFlag)
	if cert == "" {
		cert = strings.TrimSpace(os.Getenv("SHELLWAVE_TLS_CERT"))
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("SHELLWAVE_TLS_KEY"))
	}
	if cert == "" && key == "" {
		return "", "", nil
	}
	if cert == "" || key == "" {
		return "", "", errors.New("both TLS certificate and key are required")
	}
	return cert, key, nil
}

func toWSHostKey(details ssh.HostKeyDetails) wsproto.HostKeyDetails {
	return wsproto.HostKeyDetails{
		Host:                    details.Host,
		Port:                    details.Port,
		KeyType:                 details.KeyType,
		FingerprintSHA256:       details.FingerprintSHA256,
		PublicKey:               details.PublicKey,
		KnownFingerprintSHA256:  details.KnownFingerprintSHA256,
		KnownPublicKeyAvailable: details.KnownPublicKeyAvailable,
	}
}

func main() {
	flag.Parse()

	appStore, err := store.Open(*dataPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	allowPublicHosts := strings.EqualFold(os.Getenv("SHELLWAVE_ALLOW_PUBLIC_HOSTS"), "true")
	var allowedExtraHosts []string
	if extra := os.Getenv("SHELLWAVE_HOST_ALLOWLIST_EXTRA"); extra != "" {
		for _, h := range strings.Split(extra, ",") {
			if trimmed := strings.TrimSpace(h); trimmed != "" {
				allowedExtraHosts = append(allowedExtraHosts, trimmed)
			}
		}
	}

	mux := http.NewServeMux()
	api := &httpapi.API{
		Store:             appStore,
		AllowPublicHosts:  allowPublicHosts,
		AllowedExtraHosts: allowedExtraHosts,
		TrustProxy:        strings.EqualFold(os.Getenv("SHELLWAVE_TRUST_PROXY"), "true"),
	}
	mux.HandleFunc("/ws", handleWS(appStore, api))
	mux.HandleFunc("/ws/terminal", handleWS(appStore, api))
	api.Register(mux)

	// Static files
	fileServer := http.FileServer(http.Dir(*staticPath))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(*staticPath, r.URL.Path)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			// Serve index.html for SPA routing
			http.ServeFile(w, r, filepath.Join(*staticPath, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	handler := securityHeaders(mux)
	certFile, keyFile, err := resolveTLSFiles(*tlsCert, *tlsKey)
	if err != nil {
		log.Fatalf("TLS config: %v", err)
	}
	if certFile != "" {
		log.Printf("Shellwave on https://%s", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, certFile, keyFile, handler))
	}
	log.Printf("Shellwave on http://%s", *addr)
	log.Printf("WARNING: running without TLS. Password auth and terminal messages are not encrypted on the network; use -tls-cert/-tls-key, SHELLWAVE_TLS_CERT/SHELLWAVE_TLS_KEY, or a trusted HTTPS reverse proxy for non-loopback access.")
	log.Fatal(http.ListenAndServe(*addr, handler))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
