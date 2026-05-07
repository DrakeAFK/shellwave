package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shellwave/internal/devices"

	_ "modernc.org/sqlite"
)

type Store struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
}

type dataFile struct {
	Devices []devices.Device `json:"devices"`
}

type KnownHost struct {
	ID                string    `json:"id"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	KeyType           string    `json:"keyType"`
	FingerprintSHA256 string    `json:"fingerprintSha256"`
	PublicKey         string    `json:"publicKey"`
	TrustedAt         time.Time `json:"trustedAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Session struct {
	ID        string    `json:"id"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func Open(path string) (*Store, error) {
	dbPath, jsonMigrationPath, err := resolveStorePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{path: dbPath, db: db}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if jsonMigrationPath != "" {
		if err := store.importJSONDevices(jsonMigrationPath); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func DefaultPath() (string, error) {
	base, err := configBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "shellwave", "shellwave.db"), nil
}

func oldDefaultJSONPath() (string, error) {
	base, err := configBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "shellwave", "shellwave.json"), nil
}

func configBase() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base != "" {
		return base, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func resolveStorePath(path string) (dbPath string, jsonMigrationPath string, err error) {
	if path == "" {
		dbPath, err = DefaultPath()
		if err != nil {
			return "", "", err
		}
		oldJSON, err := oldDefaultJSONPath()
		if err != nil {
			return "", "", err
		}
		if fileExists(oldJSON) && !fileExists(dbPath) {
			jsonMigrationPath = oldJSON
		}
		return dbPath, jsonMigrationPath, nil
	}

	if fileExists(path) {
		sqlite, err := isSQLiteFile(path)
		if err != nil {
			return "", "", err
		}
		if !sqlite {
			target := migrationTargetPath(path)
			if !fileExists(target) {
				jsonMigrationPath = path
			}
			return target, jsonMigrationPath, nil
		}
	}
	return path, "", nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isSQLiteFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	header := make([]byte, 16)
	n, err := f.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return n == 16 && string(header) == "SQLite format 3\x00", nil
}

func migrationTargetPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return strings.TrimSuffix(path, filepath.Ext(path)) + ".db"
	}
	return path + ".db"
}

func (s *Store) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);`); err != nil {
		return err
	}

	var version int
	err = tx.QueryRow(`SELECT version FROM schema_migrations WHERE version = 1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(migrationV1); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?)`, formatTime(time.Now().UTC())); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListDevices() ([]devices.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.dedupeTailscaleDevicesLocked(); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at FROM devices ORDER BY favorite DESC, lower(name), lower(host)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []devices.Device
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, device)
	}
	return out, rows.Err()
}

func (s *Store) GetDevice(id string) (devices.Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getDeviceLocked(id)
}

func (s *Store) getDeviceLocked(id string) (devices.Device, bool) {
	row := s.db.QueryRow(`SELECT id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at FROM devices WHERE id = ?`, id)
	device, err := scanDevice(row)
	return device, err == nil
}

func (s *Store) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	return err
}

func (s *Store) UpsertDevice(device devices.Device) (devices.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	original := device
	device = devices.Normalize(device)
	if existing, ok := s.getDeviceLocked(device.ID); ok {
		device.CreatedAt = existing.CreatedAt
		if original.Source == "" {
			device.Source = existing.Source
		}
		if original.TailscaleIP == "" {
			device.TailscaleIP = existing.TailscaleIP
		}
		if original.MagicDNS == "" {
			device.MagicDNS = existing.MagicDNS
		}
		if original.OS == "" {
			device.OS = existing.OS
		}
		if original.LastSeen.IsZero() {
			device.LastSeen = existing.LastSeen
		}
		if len(original.Tags) == 0 {
			device.Tags = existing.Tags
		}
	}
	if err := s.upsertDeviceLocked(device); err != nil {
		return devices.Device{}, err
	}
	return device, nil
}

func (s *Store) MergeDevices(items []devices.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range items {
		item.Source = "tailscale"
		item.AuthMode = "password"
		item.KeyPath = ""
		item = devices.Normalize(item)

		existing, ok := s.getDeviceLocked(item.ID)
		if !ok {
			existing, ok = s.findTailnetDeviceLocked(item)
			if ok {
				item.ID = existing.ID
				if item.User == "" || item.User == "root" {
					item.User = existing.User
				}
				if item.Port == 0 || item.Port == 22 {
					item.Port = existing.Port
				}
			}
		}
		if ok {
			item.CreatedAt = existing.CreatedAt
			item.Favorite = existing.Favorite
			item.Notes = existing.Notes
		}
		if err := s.upsertDeviceLocked(item); err != nil {
			return err
		}
	}
	return s.dedupeTailscaleDevicesLocked()
}

func (s *Store) UpdateTailscaleMetadata(items []devices.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range items {
		existing, ok := s.getDeviceLocked(item.ID)
		if !ok {
			existing, ok = s.findTailnetDeviceLocked(item)
		}
		if !ok || existing.Source != "tailscale" {
			continue
		}
		existing.Name = item.Name
		existing.Host = item.Host
		existing.TailscaleIP = item.TailscaleIP
		existing.MagicDNS = item.MagicDNS
		existing.Online = item.Online
		existing.LastSeen = item.LastSeen
		existing.Tags = item.Tags
		existing.OS = item.OS
		existing.Source = "tailscale"
		existing.AuthMode = "password"
		existing.KeyPath = ""
		existing = devices.Normalize(existing)
		if err := s.upsertDeviceLocked(existing); err != nil {
			return err
		}
	}
	return s.dedupeTailscaleDevicesLocked()
}

func (s *Store) findTailnetDeviceLocked(item devices.Device) (devices.Device, bool) {
	if item.TailscaleIP != "" {
		row := s.db.QueryRow(`SELECT id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at FROM devices WHERE source = 'tailscale' AND tailscale_ip = ?`, item.TailscaleIP)
		if device, err := scanDevice(row); err == nil {
			return device, true
		}
	}
	if item.MagicDNS != "" {
		row := s.db.QueryRow(`SELECT id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at FROM devices WHERE source = 'tailscale' AND magic_dns = ?`, item.MagicDNS)
		if device, err := scanDevice(row); err == nil {
			return device, true
		}
	}
	return devices.Device{}, false
}

func (s *Store) DedupeTailscaleDevices() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dedupeTailscaleDevicesLocked()
}

func (s *Store) dedupeTailscaleDevicesLocked() error {
	rows, err := s.db.Query(`SELECT id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at FROM devices WHERE source = 'tailscale'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []devices.Device
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return err
		}
		items = append(items, device)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	groups := groupTailnetDuplicates(items)
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		canonical := group[0]
		for _, candidate := range group[1:] {
			if preferTailnetDevice(candidate, canonical) {
				canonical = candidate
			}
		}
		for _, item := range group {
			if item.ID == canonical.ID {
				continue
			}
			canonical = mergeTailnetDevice(canonical, item)
		}
		canonical.Source = "tailscale"
		canonical.AuthMode = "password"
		canonical.KeyPath = ""
		canonical = devices.Normalize(canonical)
		for _, item := range group {
			if item.ID == canonical.ID {
				continue
			}
			if _, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, item.ID); err != nil {
				return err
			}
		}
		if err := s.upsertDeviceLocked(canonical); err != nil {
			return err
		}
	}
	return nil
}

func groupTailnetDuplicates(items []devices.Device) [][]devices.Device {
	type group struct {
		devices []devices.Device
		keys    map[string]struct{}
	}
	var groups []group
	for _, item := range items {
		keys := tailnetIdentityKeys(item)
		if len(keys) == 0 {
			groups = append(groups, group{devices: []devices.Device{item}, keys: map[string]struct{}{}})
			continue
		}

		matches := []int{}
		for i := range groups {
			for _, key := range keys {
				if _, ok := groups[i].keys[key]; ok {
					matches = append(matches, i)
					break
				}
			}
		}

		if len(matches) == 0 {
			next := group{devices: []devices.Device{item}, keys: map[string]struct{}{}}
			for _, key := range keys {
				next.keys[key] = struct{}{}
			}
			groups = append(groups, next)
			continue
		}

		target := matches[0]
		groups[target].devices = append(groups[target].devices, item)
		for _, key := range keys {
			groups[target].keys[key] = struct{}{}
		}
		for i := len(matches) - 1; i >= 1; i-- {
			idx := matches[i]
			groups[target].devices = append(groups[target].devices, groups[idx].devices...)
			for key := range groups[idx].keys {
				groups[target].keys[key] = struct{}{}
			}
			groups = append(groups[:idx], groups[idx+1:]...)
		}
	}

	out := make([][]devices.Device, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.devices)
	}
	return out
}

func tailnetIdentityKeys(device devices.Device) []string {
	var keys []string
	add := func(prefix, value string) {
		value = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
		if value != "" {
			keys = append(keys, prefix+value)
		}
	}
	add("ip:", device.TailscaleIP)
	add("dns:", device.MagicDNS)
	if isTailscaleIP(device.Host) {
		add("ip:", device.Host)
	} else if looksLikeMagicDNS(device.Host) {
		add("dns:", device.Host)
	}
	return keys
}

func preferTailnetDevice(candidate, current devices.Device) bool {
	candidateScore := tailnetDeviceScore(candidate)
	currentScore := tailnetDeviceScore(current)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	return candidate.UpdatedAt.After(current.UpdatedAt)
}

func tailnetDeviceScore(device devices.Device) int {
	score := 0
	if device.Host != "" && device.TailscaleIP != "" && device.Host == device.TailscaleIP {
		score += 100
	} else if isTailscaleIP(device.Host) {
		score += 90
	}
	if device.TailscaleIP != "" {
		score += 20
	}
	if device.MagicDNS != "" {
		score += 10
	}
	if device.Online {
		score += 5
	}
	return score
}

func mergeTailnetDevice(canonical, duplicate devices.Device) devices.Device {
	if canonical.Name == "" {
		canonical.Name = duplicate.Name
	}
	if canonical.Host == "" || (!isTailscaleIP(canonical.Host) && isTailscaleIP(duplicate.Host)) {
		canonical.Host = duplicate.Host
	}
	if canonical.TailscaleIP == "" {
		canonical.TailscaleIP = duplicate.TailscaleIP
	}
	if canonical.MagicDNS == "" {
		canonical.MagicDNS = duplicate.MagicDNS
	}
	if (canonical.User == "" || canonical.User == "root") && duplicate.User != "" {
		canonical.User = duplicate.User
	}
	if (canonical.Port == 0 || canonical.Port == 22) && duplicate.Port != 0 {
		canonical.Port = duplicate.Port
	}
	if canonical.OS == "" {
		canonical.OS = duplicate.OS
	}
	canonical.Favorite = canonical.Favorite || duplicate.Favorite
	if canonical.Notes == "" {
		canonical.Notes = duplicate.Notes
	}
	if duplicate.Online {
		canonical.Online = true
	}
	if duplicate.LastSeen.After(canonical.LastSeen) {
		canonical.LastSeen = duplicate.LastSeen
	}
	if canonical.CreatedAt.IsZero() || (!duplicate.CreatedAt.IsZero() && duplicate.CreatedAt.Before(canonical.CreatedAt)) {
		canonical.CreatedAt = duplicate.CreatedAt
	}
	canonical.Tags = mergeTags(canonical.Tags, duplicate.Tags)
	return canonical
}

func mergeTags(left, right []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, tag := range append(left, right...) {
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func isTailscaleIP(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

func looksLikeMagicDNS(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
	return strings.Contains(value, ".tail")
}

func (s *Store) TrustKnownHost(host KnownHost) (KnownHost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if host.Port == 0 {
		host.Port = 22
	}
	if host.ID == "" {
		host.ID = devices.NewID(host.Host, fmt.Sprintf("%d", host.Port), host.FingerprintSHA256)
	}
	if host.TrustedAt.IsZero() {
		host.TrustedAt = now
	}
	host.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO known_hosts (id, host, port, key_type, fingerprint_sha256, public_key, trusted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host, port) DO UPDATE SET
			id = excluded.id,
			key_type = excluded.key_type,
			fingerprint_sha256 = excluded.fingerprint_sha256,
			public_key = excluded.public_key,
			updated_at = excluded.updated_at`,
		host.ID, host.Host, host.Port, host.KeyType, host.FingerprintSHA256, host.PublicKey, formatTime(host.TrustedAt), formatTime(host.UpdatedAt),
	)
	if err != nil {
		return KnownHost{}, err
	}
	return host, nil
}

func (s *Store) FindKnownHost(host string, port int) (KnownHost, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findKnownHostLocked(host, port)
}

func (s *Store) findKnownHostLocked(host string, port int) (KnownHost, bool, error) {
	if port == 0 {
		port = 22
	}
	row := s.db.QueryRow(`SELECT id, host, port, key_type, fingerprint_sha256, public_key, trusted_at, updated_at FROM known_hosts WHERE host = ? AND port = ?`, host, port)
	hostKey, err := scanKnownHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return KnownHost{}, false, nil
	}
	if err != nil {
		return KnownHost{}, false, err
	}
	return hostKey, true, nil
}

func (s *Store) ListKnownHosts() ([]KnownHost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, host, port, key_type, fingerprint_sha256, public_key, trusted_at, updated_at FROM known_hosts ORDER BY lower(host), port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []KnownHost
	for rows.Next() {
		host, err := scanKnownHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, host)
	}
	return out, rows.Err()
}

func (s *Store) DeleteKnownHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM known_hosts WHERE id = ?`, id)
	return err
}

func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now().UTC()),
	)
	return err
}

func (s *Store) GetSetting(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) CreateSession(session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.ID == "" {
		session.ID = devices.NewID(session.TokenHash, formatTime(session.ExpiresAt))
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO sessions (id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		session.ID, session.TokenHash, formatTime(session.CreatedAt), formatTime(session.ExpiresAt),
	)
	return err
}

func (s *Store) GetSessionByTokenHash(tokenHash string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id, token_hash, created_at, expires_at FROM sessions WHERE token_hash = ?`, tokenHash)
	var session Session
	var createdAt, expiresAt string
	err := row.Scan(&session.ID, &session.TokenHash, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	session.CreatedAt = parseTime(createdAt)
	session.ExpiresAt = parseTime(expiresAt)
	return session, true, nil
}

func (s *Store) DeleteSessionByTokenHash(tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(now.UTC()))
	return err
}

func (s *Store) importJSONDevices(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || len(data) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	var legacy dataFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("read legacy JSON store: %w", err)
	}
	for _, device := range legacy.Devices {
		if _, err := s.UpsertDevice(device); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) upsertDeviceLocked(device devices.Device) error {
	tags, err := json.Marshal(device.Tags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO devices (id, name, host, tailscale_ip, magic_dns, user, port, auth_mode, key_path, source, online, last_seen, tags_json, os, favorite, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			host = excluded.host,
			tailscale_ip = excluded.tailscale_ip,
			magic_dns = excluded.magic_dns,
			user = excluded.user,
			port = excluded.port,
			auth_mode = excluded.auth_mode,
			key_path = excluded.key_path,
			source = excluded.source,
			online = excluded.online,
			last_seen = excluded.last_seen,
			tags_json = excluded.tags_json,
			os = excluded.os,
			favorite = excluded.favorite,
			notes = excluded.notes,
			updated_at = excluded.updated_at`,
		device.ID,
		device.Name,
		device.Host,
		nullableString(device.TailscaleIP),
		nullableString(device.MagicDNS),
		device.User,
		device.Port,
		device.AuthMode,
		nullableString(device.KeyPath),
		device.Source,
		boolInt(device.Online),
		nullableTime(device.LastSeen),
		string(tags),
		nullableString(device.OS),
		boolInt(device.Favorite),
		nullableString(device.Notes),
		formatTime(device.CreatedAt),
		formatTime(device.UpdatedAt),
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(row scanner) (devices.Device, error) {
	var d devices.Device
	var tailscaleIP, magicDNS, keyPath, lastSeen, tagsJSON, osName, notes sql.NullString
	var createdAt, updatedAt string
	var online, favorite int
	if err := row.Scan(&d.ID, &d.Name, &d.Host, &tailscaleIP, &magicDNS, &d.User, &d.Port, &d.AuthMode, &keyPath, &d.Source, &online, &lastSeen, &tagsJSON, &osName, &favorite, &notes, &createdAt, &updatedAt); err != nil {
		return devices.Device{}, err
	}
	d.TailscaleIP = tailscaleIP.String
	d.MagicDNS = magicDNS.String
	d.KeyPath = keyPath.String
	d.Online = online == 1
	d.OS = osName.String
	d.Favorite = favorite == 1
	d.Notes = notes.String
	if lastSeen.Valid {
		d.LastSeen = parseTime(lastSeen.String)
	}
	if tagsJSON.Valid && tagsJSON.String != "" {
		_ = json.Unmarshal([]byte(tagsJSON.String), &d.Tags)
	}
	d.CreatedAt = parseTime(createdAt)
	d.UpdatedAt = parseTime(updatedAt)
	return d, nil
}

func scanKnownHost(row scanner) (KnownHost, error) {
	var host KnownHost
	var trustedAt, updatedAt string
	if err := row.Scan(&host.ID, &host.Host, &host.Port, &host.KeyType, &host.FingerprintSHA256, &host.PublicKey, &trustedAt, &updatedAt); err != nil {
		return KnownHost{}, err
	}
	host.TrustedAt = parseTime(trustedAt)
	host.UpdatedAt = parseTime(updatedAt)
	return host, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

const migrationV1 = `
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  host TEXT NOT NULL,
  tailscale_ip TEXT,
  magic_dns TEXT,
  user TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  auth_mode TEXT NOT NULL DEFAULT 'agent',
  key_path TEXT,
  source TEXT NOT NULL DEFAULT 'manual',
  online INTEGER NOT NULL DEFAULT 0,
  last_seen TEXT,
  tags_json TEXT,
  os TEXT,
  favorite INTEGER NOT NULL DEFAULT 0,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS known_hosts (
  id TEXT PRIMARY KEY,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  key_type TEXT NOT NULL,
  fingerprint_sha256 TEXT NOT NULL,
  public_key TEXT NOT NULL,
  trusted_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(host, port)
);

CREATE TABLE IF NOT EXISTS saved_commands (
  id TEXT PRIMARY KEY,
  device_id TEXT,
  name TEXT NOT NULL,
  description TEXT,
  command TEXT NOT NULL,
  category TEXT,
  dangerous INTEGER NOT NULL DEFAULT 0,
  requires_confirm INTEGER NOT NULL DEFAULT 0,
  timeout_seconds INTEGER NOT NULL DEFAULT 30,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS command_history (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  command TEXT NOT NULL,
  exit_code INTEGER,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  duration_ms INTEGER,
  source TEXT,
  saved_command_id TEXT
);

CREATE TABLE IF NOT EXISTS command_runs (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  command TEXT NOT NULL,
  status TEXT NOT NULL,
  exit_code INTEGER,
  stdout_preview TEXT,
  stderr_preview TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  duration_ms INTEGER
);

CREATE TABLE IF NOT EXISTS log_presets (
  id TEXT PRIMARY KEY,
  device_id TEXT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_devices_source ON devices(source);
CREATE INDEX IF NOT EXISTS idx_command_history_device_started ON command_history(device_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_runs_device_started ON command_runs(device_id, started_at DESC);
`
