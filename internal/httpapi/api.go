package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"shellwave/internal/devices"
	"shellwave/internal/ssh"
	"shellwave/internal/store"
	"shellwave/internal/tailscale"

	"golang.org/x/crypto/bcrypt"
)

type API struct {
	Store             *store.Store
	Tailscale         func(context.Context) (tailscale.Status, error)
	AllowPublicHosts  bool
	AllowedExtraHosts []string
	TrustProxy        bool

	authMu        sync.Mutex
	loginAttempts map[string][]time.Time
	sshAttempts   map[string][]time.Time
}

type errorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	HostKey *ssh.HostKeyDetails `json:"hostKey,omitempty"`
}

func (api *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", api.health)
	mux.HandleFunc("/api/auth/status", api.authStatus)
	mux.HandleFunc("/api/auth/setup", api.authSetup)
	mux.HandleFunc("/api/auth/login", api.authLogin)
	mux.HandleFunc("/api/auth/logout", api.authLogout)
	mux.HandleFunc("/api/devices", api.requireAuth(api.devices))
	mux.HandleFunc("/api/devices/", api.requireAuth(api.deviceAction))
	mux.HandleFunc("/api/known-hosts", api.requireAuth(api.knownHosts))
	mux.HandleFunc("/api/known-hosts/trust", api.requireAuth(api.trustKnownHost))
	mux.HandleFunc("/api/known-hosts/", api.requireAuth(api.knownHostByID))
	mux.HandleFunc("/api/tailscale/status", api.requireAuth(api.tailscaleStatus))
	mux.HandleFunc("/api/tailscale/import", api.requireAuth(api.importTailscale))
}

func (api *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) authStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	setupRequired, err := api.setupRequired()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"setupRequired": setupRequired,
		"authenticated": !setupRequired && api.IsAuthenticated(r),
	})
}

func (api *API) authSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	setupRequired, err := api.setupRequired()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if !setupRequired {
		writeError(w, http.StatusConflict, "already_configured", "Admin password is already configured")
		return
	}
	var req passwordAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", err.Error())
		return
	}
	if err := api.Store.SetSetting("admin_password_hash", string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if err := api.issueSession(w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (api *API) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !api.allowLoginAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts. Try again shortly.")
		return
	}
	hash, ok, err := api.Store.GetSetting("admin_password_hash")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "setup_required", "Create the admin password first")
		return
	}
	var req passwordAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid password")
		return
	}
	api.clearLoginAttempts(r)
	if err := api.issueSession(w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = api.Store.DeleteSessionByTokenHash(hashToken(cookie.Value))
	}
	http.SetCookie(w, expiredSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setupRequired, err := api.setupRequired()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		if setupRequired {
			writeError(w, http.StatusUnauthorized, "setup_required", "Create the admin password first")
			return
		}
		if !api.IsAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Login required")
			return
		}
		next(w, r)
	}
}

func (api *API) IsAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	session, ok, err := api.Store.GetSessionByTokenHash(hashToken(cookie.Value))
	if err != nil || !ok {
		return false
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = api.Store.DeleteSessionByTokenHash(session.TokenHash)
		return false
	}
	return true
}

func (api *API) setupRequired() (bool, error) {
	_, ok, err := api.Store.GetSetting("admin_password_hash")
	return !ok, err
}

func (api *API) issueSession(w http.ResponseWriter, r *http.Request) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	_ = api.Store.DeleteExpiredSessions(time.Now().UTC())
	if err := api.Store.CreateSession(store.Session{
		TokenHash: hashToken(token),
		ExpiresAt: expires,
	}); err != nil {
		return err
	}
	http.SetCookie(w, sessionCookie(r, token, expires))
	return nil
}

func (api *API) allowLoginAttempt(r *http.Request) bool {
	api.authMu.Lock()
	defer api.authMu.Unlock()
	if api.loginAttempts == nil {
		api.loginAttempts = map[string][]time.Time{}
	}
	now := time.Now().UTC()
	windowStart := now.Add(-5 * time.Minute)
	pruneAttemptMap(api.loginAttempts, windowStart)
	key := api.clientIP(r)
	attempts := api.loginAttempts[key]
	filtered := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(windowStart) {
			filtered = append(filtered, attempt)
		}
	}
	if len(filtered) >= 8 {
		api.loginAttempts[key] = filtered
		return false
	}
	api.loginAttempts[key] = append(filtered, now)
	return true
}

func (api *API) clearLoginAttempts(r *http.Request) {
	api.authMu.Lock()
	defer api.authMu.Unlock()
	delete(api.loginAttempts, api.clientIP(r))
}

func (api *API) AllowSSHAttempt(r *http.Request) bool {
	api.authMu.Lock()
	defer api.authMu.Unlock()
	if api.sshAttempts == nil {
		api.sshAttempts = map[string][]time.Time{}
	}
	now := time.Now().UTC()
	windowStart := now.Add(-1 * time.Minute)
	pruneAttemptMap(api.sshAttempts, windowStart)
	key := api.clientIP(r)
	attempts := api.sshAttempts[key]
	filtered := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(windowStart) {
			filtered = append(filtered, attempt)
		}
	}
	if len(filtered) >= 20 {
		api.sshAttempts[key] = filtered
		return false
	}
	api.sshAttempts[key] = append(filtered, now)
	return true
}

func (api *API) CheckHostAllowed(ctx context.Context, host string) error {
	if api.AllowPublicHosts {
		return nil
	}

	// Check if it matches allowed extra hosts exactly
	for _, allowed := range api.AllowedExtraHosts {
		if strings.EqualFold(host, allowed) {
			return nil
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		if api.ipAllowed(ip) {
			return nil
		}
		return errors.New("connection to public host is not allowed by policy")
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("host resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("no IP addresses found for host")
	}

	for _, resolved := range ips {
		if !api.ipAllowed(resolved.IP) {
			return errors.New("connection to public host is not allowed by policy")
		}
	}
	return nil
}

func (api *API) ipAllowed(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	if cgnat.Contains(ip) {
		return true
	}

	for _, allowed := range api.AllowedExtraHosts {
		_, ipNet, err := net.ParseCIDR(allowed)
		if err == nil && ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

func (api *API) devices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		devices, err := api.Store.ListDevices()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
	case http.MethodPost:
		var req deviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
			return
		}
		if strings.TrimSpace(req.Host) == "" {
			writeError(w, http.StatusBadRequest, "missing_host", "Host is required")
			return
		}
		device, err := api.Store.UpsertDevice(devices.Device{
			ID:       req.ID,
			Name:     req.Name,
			Host:     req.Host,
			User:     req.User,
			Port:     req.Port,
			AuthMode: "password",
			Source:   "manual",
			Online:   false,
			Favorite: req.Favorite,
			Notes:    req.Notes,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"device": device})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *API) deviceAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "Route not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		api.deviceByID(w, r, id)
		return
	}
	action := parts[1]
	device, ok := api.Store.GetDevice(id)
	if !ok {
		writeError(w, http.StatusNotFound, "device_not_found", "Device not found")
		return
	}

	switch action {
	case "test":
		api.testDevice(w, r, device)
	case "overview":
		api.overview(w, r, device)
	case "commands":
		api.runCommand(w, r, device)
	default:
		writeError(w, http.StatusNotFound, "not_found", "Route not found")
	}
}

func (api *API) deviceByID(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodPatch:
		existing, ok := api.Store.GetDevice(id)
		if !ok {
			writeError(w, http.StatusNotFound, "device_not_found", "Device not found")
			return
		}
		var req deviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
			return
		}
		if strings.TrimSpace(req.Name) != "" {
			existing.Name = req.Name
		}
		if strings.TrimSpace(req.Host) != "" {
			existing.Host = req.Host
		}
		if strings.TrimSpace(req.User) != "" {
			existing.User = req.User
		}
		if req.Port > 0 {
			existing.Port = req.Port
		}
		existing.AuthMode = "password"
		existing.KeyPath = ""
		existing.Favorite = req.Favorite
		existing.Notes = req.Notes
		device, err := api.Store.UpsertDevice(existing)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"device": device})
	case http.MethodDelete:
		device, ok := api.Store.GetDevice(id)
		if !ok {
			writeError(w, http.StatusNotFound, "device_not_found", "Device not found")
			return
		}
		if device.Source == "tailscale" {
			writeError(w, http.StatusBadRequest, "tailscale_device", "Tailscale devices cannot be deleted; remove them from your tailnet or hide support can be added later")
			return
		}
		if err := api.Store.DeleteDevice(id); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *API) testDevice(w http.ResponseWriter, r *http.Request, device devices.Device) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !api.AllowSSHAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many SSH connection attempts. Try again later.")
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	host := device.SSHHost()
	port := device.Port
	if port == 0 {
		port = 22
	}
	result := connectionTestResult{Host: host, Port: port}
	if req.Auth.Password == "" {
		req.Auth.Password = req.Password
	}

	if err := api.CheckHostAllowed(r.Context(), host); err != nil {
		result.Message = err.Error()
		api.updateDeviceOnline(device, false)
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "result": result})
		return
	}

	dnsCtx, cancelDNS := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancelDNS()
	if net.ParseIP(host) == nil {
		if _, err := net.DefaultResolver.LookupHost(dnsCtx, host); err != nil {
			result.Message = "DNS/host resolution failed: " + err.Error()
			api.updateDeviceOnline(device, false)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": result})
			return
		}
	}
	result.DNSOk = true

	address := net.JoinHostPort(host, portString(port))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		result.Message = "SSH port is unreachable: " + err.Error()
		api.updateDeviceOnline(device, false)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": result})
		return
	}
	_ = conn.Close()
	result.PortOpen = true
	api.updateDeviceOnline(device, true)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// Pass context to runSSH
	run, err := api.runSSH(ctx, device, req.Auth, "echo shellwave-ok", 10*time.Second)
	if err != nil {
		var hostKeyErr *ssh.HostKeyError
		if errors.As(err, &hostKeyErr) {
			details := hostKeyErr.Details
			result.HostKey = &details
			result.Message = hostKeyErr.Error()
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": result})
			return
		}
		result.Message = friendlySSHError(err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": result})
		return
	}
	result.SSHAuthOk = true
	result.CommandOk = strings.TrimSpace(run.Stdout) == "shellwave-ok" && run.ExitCode == 0
	if result.CommandOk {
		result.Message = "Connected successfully"
	} else {
		result.Message = "SSH authenticated, but the validation command did not return the expected response"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": result.CommandOk, "result": result})
}

func (api *API) updateDeviceOnline(device devices.Device, online bool) {
	device.Online = online
	if online {
		device.LastSeen = time.Now().UTC()
	}
	_, _ = api.Store.UpsertDevice(device)
}

func (api *API) overview(w http.ResponseWriter, r *http.Request, device devices.Device) {
	if r.Method == http.MethodPost {
		api.probeOverview(w, r, device)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device": device,
		"overview": map[string]any{
			"name":        device.Name,
			"host":        device.SSHHost(),
			"os":          device.OS,
			"source":      device.Source,
			"online":      device.Online,
			"lastSeen":    device.LastSeen,
			"tailscaleIp": device.TailscaleIP,
			"magicDns":    device.MagicDNS,
		},
	})
}

func (api *API) probeOverview(w http.ResponseWriter, r *http.Request, device devices.Device) {
	if !api.AllowSSHAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many SSH connection attempts. Try again later.")
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if req.Auth.Password == "" {
		req.Auth.Password = req.Password
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := api.runSSH(ctx, device, req.Auth, overviewCommand, 15*time.Second)
	if err != nil {
		api.updateDeviceOnline(device, false)
		api.writeSSHError(w, "probe_failed", err)
		return
	}
	api.updateDeviceOnline(device, true)
	writeJSON(w, http.StatusOK, map[string]any{"overview": parseOverview(result.Stdout), "result": result})
}

func (api *API) runCommand(w http.ResponseWriter, r *http.Request, device devices.Device) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !api.AllowSSHAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many SSH connection attempts. Try again later.")
		return
	}
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, "missing_command", "Command is required")
		return
	}
	if req.Auth.Password == "" {
		req.Auth.Password = req.Password
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := api.runSSH(ctx, device, req.Auth, req.Command, 15*time.Second)
	if err != nil {
		api.updateDeviceOnline(device, false)
		api.writeSSHError(w, "command_failed", err)
		return
	}
	api.updateDeviceOnline(device, true)
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (api *API) knownHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hosts, err := api.Store.ListKnownHosts()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"knownHosts": hosts})
	case http.MethodPost:
		api.trustKnownHost(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *API) trustKnownHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req trustKnownHostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.FingerprintSHA256) == "" || strings.TrimSpace(req.PublicKey) == "" {
		writeError(w, http.StatusBadRequest, "missing_host_key", "Host, fingerprint, and public key are required")
		return
	}
	host, err := api.Store.TrustKnownHost(store.KnownHost{
		Host:              req.Host,
		Port:              req.Port,
		KeyType:           req.KeyType,
		FingerprintSHA256: req.FingerprintSHA256,
		PublicKey:         req.PublicKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"knownHost": host})
}

func (api *API) knownHostByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/known-hosts/")
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found", "Known host not found")
		return
	}
	if err := api.Store.DeleteKnownHost(id); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) runSSH(ctx context.Context, device devices.Device, auth ssh.AuthConfig, command string, timeout time.Duration) (ssh.RunResult, error) {
	if err := api.CheckHostAllowed(ctx, device.SSHHost()); err != nil {
		return ssh.RunResult{ExitCode: -1}, err
	}
	auth = mergeAuth(device, auth)
	return ssh.Run(ctx, ssh.RunConfig{
		User:            device.User,
		Host:            device.SSHHost(),
		Port:            device.Port,
		Auth:            auth,
		HostKeyCallback: ssh.HostKeyCallback(api.Store, device.SSHHost(), device.Port),
		Command:         command,
		Timeout:         timeout,
	})
}

func (api *API) writeSSHError(w http.ResponseWriter, fallbackCode string, err error) {
	var hostKeyErr *ssh.HostKeyError
	if errors.As(err, &hostKeyErr) {
		writeJSON(w, http.StatusConflict, errorBody{Error: apiError{
			Code:    hostKeyErr.Kind,
			Message: hostKeyErr.Error(),
			HostKey: &ssh.HostKeyDetails{
				Host:                    hostKeyErr.Details.Host,
				Port:                    hostKeyErr.Details.Port,
				KeyType:                 hostKeyErr.Details.KeyType,
				FingerprintSHA256:       hostKeyErr.Details.FingerprintSHA256,
				PublicKey:               hostKeyErr.Details.PublicKey,
				KnownFingerprintSHA256:  hostKeyErr.Details.KnownFingerprintSHA256,
				KnownPublicKeyAvailable: hostKeyErr.Details.KnownPublicKeyAvailable,
			},
		}})
		return
	}
	writeError(w, http.StatusBadGateway, fallbackCode, friendlySSHError(err))
}

func (api *API) tailscaleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status, err := api.localTailscaleStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tailscale_failed", err.Error())
		return
	}
	if status.Available {
		if err := api.Store.UpdateTailscaleMetadata(status.Devices); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, status)
}

func (api *API) importTailscale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req tailscaleImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	status, err := api.localTailscaleStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tailscale_failed", err.Error())
		return
	}
	if !status.Available {
		writeError(w, http.StatusBadGateway, "tailscale_unavailable", status.Message)
		return
	}
	importDevices := api.prepareTailscaleImportDevices(status.Devices, req)
	if len(importDevices) == 0 {
		writeError(w, http.StatusBadRequest, "no_devices_selected", "Select at least one tailnet device to import")
		return
	}
	if err := api.Store.MergeDevices(importDevices); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	devices, err := api.Store.ListDevices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices, "imported": len(importDevices)})
}

func (api *API) localTailscaleStatus(ctx context.Context) (tailscale.Status, error) {
	if api.Tailscale != nil {
		return api.Tailscale(ctx)
	}
	return tailscale.LocalStatus(ctx)
}

func (api *API) prepareTailscaleImportDevices(discovered []devices.Device, req tailscaleImportRequest) []devices.Device {
	byID := map[string]devices.Device{}
	for _, device := range discovered {
		byID[device.ID] = device
	}

	defaultUser := strings.TrimSpace(req.DefaultUser)

	var importDevices []devices.Device
	if len(req.Devices) > 0 {
		for _, item := range req.Devices {
			device, ok := byID[item.ID]
			if !ok {
				continue
			}
			importDevices = append(importDevices, api.applyTailnetImportOptions(device, item, defaultUser, true))
		}
		return importDevices
	}

	if len(req.DeviceIDs) > 0 {
		for _, id := range req.DeviceIDs {
			device, ok := byID[id]
			if !ok {
				continue
			}
			importDevices = append(importDevices, api.applyTailnetImportOptions(device, tailscaleImportDevice{ID: id}, defaultUser, defaultUser != ""))
		}
		return importDevices
	}

	for _, device := range discovered {
		importDevices = append(importDevices, api.applyTailnetImportOptions(device, tailscaleImportDevice{ID: device.ID}, defaultUser, false))
	}
	return importDevices
}

func (api *API) applyTailnetImportOptions(device devices.Device, item tailscaleImportDevice, defaultUser string, forceDefaults bool) devices.Device {
	existing, exists := api.Store.GetDevice(device.ID)
	if exists {
		device.Favorite = existing.Favorite
		device.Notes = existing.Notes
		if !forceDefaults {
			device.User = existing.User
			device.Port = existing.Port
			device.AuthMode = existing.AuthMode
		}
	}

	if user := strings.TrimSpace(item.User); user != "" {
		device.User = user
	} else if defaultUser != "" {
		device.User = defaultUser
	} else if device.User == "" {
		device.User = "root"
	}

	if item.Port > 0 {
		device.Port = item.Port
	} else if device.Port == 0 {
		device.Port = 22
	}

	device.AuthMode = "password"
	device.KeyPath = ""
	device.Source = "tailscale"
	return device
}

type deviceRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	Favorite bool   `json:"favorite"`
	Notes    string `json:"notes"`
}

type commandRequest struct {
	Command  string         `json:"command"`
	Auth     ssh.AuthConfig `json:"auth"`
	Password string         `json:"password"`
}

type authRequest struct {
	Auth     ssh.AuthConfig `json:"auth"`
	Password string         `json:"password"`
}

type passwordAuthRequest struct {
	Password string `json:"password"`
}

type tailscaleImportRequest struct {
	DefaultUser string                  `json:"defaultUser"`
	DeviceIDs   []string                `json:"deviceIds"`
	Devices     []tailscaleImportDevice `json:"devices"`
}

type tailscaleImportDevice struct {
	ID   string `json:"id"`
	User string `json:"user"`
	Port int    `json:"port"`
}

type trustKnownHostRequest struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	KeyType           string `json:"keyType"`
	FingerprintSHA256 string `json:"fingerprintSha256"`
	PublicKey         string `json:"publicKey"`
}

type connectionTestResult struct {
	Host      string              `json:"host"`
	Port      int                 `json:"port"`
	DNSOk     bool                `json:"dnsOk"`
	PortOpen  bool                `json:"portOpen"`
	SSHAuthOk bool                `json:"sshAuthOk"`
	CommandOk bool                `json:"commandOk"`
	Message   string              `json:"message"`
	HostKey   *ssh.HostKeyDetails `json:"hostKey,omitempty"`
}

func mergeAuth(_ devices.Device, auth ssh.AuthConfig) ssh.AuthConfig {
	return ssh.AuthConfig{Type: "password", Password: auth.Password}
}

const sessionCookieName = "shellwave_session"

func randomToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sessionCookie(r *http.Request, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}
}

func expiredSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (api *API) clientIP(r *http.Request) string {
	if api.TrustProxy {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func pruneAttemptMap(attemptsByIP map[string][]time.Time, windowStart time.Time) {
	for key, attempts := range attemptsByIP {
		filtered := attempts[:0]
		for _, attempt := range attempts {
			if attempt.After(windowStart) {
				filtered = append(filtered, attempt)
			}
		}
		if len(filtered) == 0 {
			delete(attemptsByIP, key)
			continue
		}
		attemptsByIP[key] = filtered
	}
}

const overviewCommand = `printf 'user=%s\n' "$(whoami 2>/dev/null)"
printf 'hostname=%s\n' "$(hostname 2>/dev/null)"
printf 'kernel=%s\n' "$(uname -sr 2>/dev/null)"
if [ -r /etc/os-release ]; then . /etc/os-release; printf 'os=%s\n' "$PRETTY_NAME"; else printf 'os=%s\n' "$(uname -s 2>/dev/null)"; fi
printf 'uptime=%s\n' "$((uptime -p 2>/dev/null || uptime 2>/dev/null) | sed 's/^ *//')"
printf 'disk=%s\n' "$(df -h / 2>/dev/null | awk 'NR==2 {print $3 " used / " $2 " total (" $5 ")"}')"
if command -v free >/dev/null 2>&1; then printf 'memory=%s\n' "$(free -m | awk '/Mem:/ {print $3 "MB used / " $2 "MB total"}')"; else printf 'memory=%s\n' ""; fi
printf 'load=%s\n' "$(uptime 2>/dev/null | sed -E 's/.*load averages?: (.*)/\1/')"
printf 'cpuCount=%s\n' "$(nproc 2>/dev/null || echo 1)"
printf 'dockerInstalled=%s\n' "$(command -v docker >/dev/null && echo true || echo false)"
printf 'dockerContainers=%s\n' "$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')"
printf 'tailscaleInstalled=%s\n' "$(command -v tailscale >/dev/null && echo true || echo false)"
printf 'tailscaleStatus=%s\n' "$(tailscale status 2>/dev/null >/dev/null && echo true || echo false)"
printf 'portsCount=%s\n' "$((ss -tulpen 2>/dev/null || netstat -tulpen 2>/dev/null) | grep -c LISTEN 2>/dev/null || echo 0)"`

func parseOverview(output string) map[string]string {
	overview := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		overview[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return overview
}

func friendlySSHError(err error) string {
	message := err.Error()
	if strings.Contains(message, "unable to authenticate") {
		return "SSH authentication failed. Check the session password, SSH user, and whether password login is enabled on the server."
	}
	if strings.Contains(message, "connection refused") {
		return "SSH connection refused. Check the host, port, and firewall."
	}
	if strings.Contains(message, "i/o timeout") || strings.Contains(message, "no route to host") {
		return "SSH host is unreachable. Check the address, Tailscale connectivity, and firewall."
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

func portString(port int) string {
	if port == 0 {
		return "22"
	}
	return strconv.Itoa(port)
}
