package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"shellwave/internal/devices"
	"shellwave/internal/store"
	"shellwave/internal/tailscale"
)

func TestAuthSetupLoginAndProtectedAPI(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	mux := http.NewServeMux()
	api := &API{Store: s}
	api.Register(mux)

	status := doJSON(t, mux, http.MethodGet, "/api/auth/status", nil, nil)
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d", status.Code)
	}
	var statusBody map[string]bool
	if err := json.NewDecoder(status.Body).Decode(&statusBody); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !statusBody["setupRequired"] || statusBody["authenticated"] {
		t.Fatalf("unexpected initial auth status: %#v", statusBody)
	}

	unauthorized := doJSON(t, mux, http.MethodGet, "/api/devices", nil, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected API to require setup/login, got %d", unauthorized.Code)
	}

	setup := doJSON(t, mux, http.MethodPost, "/api/auth/setup", map[string]string{"password": "super-secret"}, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup code = %d body=%s", setup.Code, setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]

	devices := doJSON(t, mux, http.MethodGet, "/api/devices", nil, cookie)
	if devices.Code != http.StatusOK {
		t.Fatalf("expected authenticated devices request, got %d", devices.Code)
	}

	logout := doJSON(t, mux, http.MethodPost, "/api/auth/logout", nil, cookie)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout code = %d", logout.Code)
	}
	afterLogout := doJSON(t, mux, http.MethodGet, "/api/devices", nil, cookie)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("expected logout to revoke session, got %d", afterLogout.Code)
	}

	badLogin := doJSON(t, mux, http.MethodPost, "/api/auth/login", map[string]string{"password": "wrong"}, nil)
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("bad login code = %d", badLogin.Code)
	}
	goodLogin := doJSON(t, mux, http.MethodPost, "/api/auth/login", map[string]string{"password": "super-secret"}, nil)
	if goodLogin.Code != http.StatusOK {
		t.Fatalf("good login code = %d body=%s", goodLogin.Code, goodLogin.Body.String())
	}
}

func TestSelectiveTailscaleImport(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "shellwave.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	api := &API{
		Store: s,
		Tailscale: func(context.Context) (tailscale.Status, error) {
			return tailscale.Status{
				Available: true,
				Devices: []devices.Device{
					devices.Normalize(devices.Device{ID: "server-1", Name: "server", Host: "server.tail.ts.net", TailscaleIP: "100.64.0.20", User: "root", Port: 22, AuthMode: "password", Source: "tailscale", Online: true, OS: "linux"}),
					devices.Normalize(devices.Device{ID: "nas-1", Name: "nas", Host: "nas.tail.ts.net", TailscaleIP: "100.64.0.21", User: "root", Port: 22, AuthMode: "password", Source: "tailscale", Online: false, OS: "linux"}),
				},
			}, nil
		},
	}
	mux := http.NewServeMux()
	api.Register(mux)
	cookie := setupTestSession(t, mux)

	res := doJSON(t, mux, http.MethodPost, "/api/tailscale/import", map[string]any{
		"defaultUser":     "drake",
		"defaultAuthMode": "password",
		"devices": []map[string]any{
			{"id": "server-1", "user": "ubuntu", "port": 2222},
		},
	}, cookie)
	if res.Code != http.StatusOK {
		t.Fatalf("import code = %d body=%s", res.Code, res.Body.String())
	}

	server, ok := s.GetDevice("server-1")
	if !ok {
		t.Fatal("expected selected device to be imported")
	}
	if server.User != "ubuntu" || server.Port != 2222 || server.AuthMode != "password" || server.Source != "tailscale" {
		t.Fatalf("unexpected imported server: %#v", server)
	}
	if _, ok := s.GetDevice("nas-1"); ok {
		t.Fatal("did not expect unselected device to be imported")
	}
}

func setupTestSession(t *testing.T, mux http.Handler) *http.Cookie {
	t.Helper()
	setup := doJSON(t, mux, http.MethodPost, "/api/auth/setup", map[string]string{"password": "super-secret"}, nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup code = %d body=%s", setup.Code, setup.Body.String())
	}
	cookies := setup.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected setup cookie")
	}
	return cookies[0]
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
