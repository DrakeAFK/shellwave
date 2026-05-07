
# Shellwave Build Plan

## Agent mission

Continue building Shellwave into a working, polished, self-hosted, Tailscale-friendly machine console.

Shellwave is aimed first at personal homelab / power users, with a future path toward an open-source/commercial developer tool. It should not merely be “SSH in a browser.” It should become the fastest way to discover, inspect, connect to, and operate machines in a tailnet.

The app should be:

- Agentless-first.
- Tailscale-friendly.
- Safe by default.
- Useful before opening a terminal.
- Fast and keyboard-native.
- Visually polished like a modern YC-style devtool.
- Built for Docker/Linux deployment, while keeping Mac development smooth.

Current project state includes some foundational work already:

- Go backend serving Svelte frontend.
- Structured WebSocket terminal protocol.
- Basic SSH terminal session.
- Basic device model.
- Local device persistence.
- Tailscale CLI discovery/import.
- Basic HTTP API.
- Basic command runner.
- Some backend tests.

## Current alpha decisions

These decisions override older roadmap notes below until the product direction changes:

- SSH transport stays, but ShellWave supports password authentication only.
- SSH agent auth and key-path auth are intentionally out of scope for this alpha.
- Tailscale import should include the ShellWave host itself when Tailscale reports it.
- Tailscale devices should connect by Tailscale IP when available, while keeping MagicDNS as display metadata.

This plan continues from the current state and completes the product.

---

# Product vision

Shellwave should feel like:

> Linear / Vercel / Raycast / Supabase / Neon for tailnet machines.

It should let a user:

1. Open Shellwave.
2. Discover tailnet devices.
3. Add manual devices.
4. See machine health at a glance.
5. Open a reliable browser terminal.
6. Run saved commands and safe runbooks.
7. View logs.
8. Browse files read-only.
9. Inspect Docker containers.
10. View services and processes.
11. Avoid memorizing SSH commands.

Terminal is important, but it should not be the entire product.

---

# Product scope

## Now

Shellwave should target:

- Personal homelab users.
- Power users.
- Solo developers.
- Tailscale users.
- People who manage a few Linux/Mac machines and Docker hosts.

## Later

Shellwave may evolve into:

- Open-source developer tool.
- Small-team internal ops console.
- Commercial/SaaS-adjacent tool.
- Optional remote agent platform.

Do not overbuild enterprise multi-user features yet.

---

# Core principles

## Agentless-first

Use SSH for all remote operations in the MVP:

- Terminal -> SSH PTY session.
- Overview -> SSH command probes.
- Commands -> SSH non-interactive runner.
- Logs -> SSH command runner / streaming later.
- Files -> SFTP read-only first.
- Docker -> remote Docker CLI.
- Services -> remote `systemctl` read-only first.
- Processes -> remote `ps` read-only first.

Do not build a remote agent yet.

Optional remote agent can be considered later for:

- Live charts.
- Background monitoring.
- Notifications.
- Job tracking.
- File indexing.
- Multi-user audit/compliance.

## Tailscale integration

Use local Tailscale CLI now:

```bash
tailscale status --json
````

Support Tailscale API later, but do not require it for MVP.

## Security

Default posture:

* Do not store raw passwords.
* Do not put secrets in URLs.
* Do not log secrets.
* Do not use `ssh.InsecureIgnoreHostKey()` in production paths.
* WebSocket origin must not be open to all origins.
* App should require a single local admin password.
* Dangerous commands must require confirmation.

## Persistence

Move from the current JSON-file store to SQLite before building more product features.

Reason: saved commands, command history, known hosts, settings, login sessions, and log presets need structured persistence.

Use a pure-Go SQLite driver if possible.

Default DB path:

```txt
~/.config/shellwave/shellwave.db
```

Preserve `-data` flag support.

---

# Recommended build order

Build in this order:

1. Reliable core/security.
2. SQLite migration.
3. SSH auth + known hosts.
4. Device onboarding/import UI.
5. Device overview cards.
6. Command palette + saved commands.
7. Logs viewer.
8. Files read-only.
9. Docker panel.
10. Services/processes read-only.
11. App login/security.
12. Full UI polish.
13. Mobile/responsive polish.
14. Tests and QA.

Do not jump ahead to flashy features before terminal/session/auth/security are reliable.

---

# Phase 1 — Reliable core

## Goal

Make the existing core trustworthy before adding more product surface.

## 1.1 Terminal lifecycle

Ensure the terminal WebSocket is stable and predictable.

Tasks:

* Terminal must connect only after sending a structured `connect` message.
* Password must never appear in the WebSocket URL.
* Switching devices must close the previous socket/session.
* Terminal resize must be debounced and sent after xterm fit.
* Reconnect button must work.
* Terminal must show explicit states:

  * `idle`
  * `connecting`
  * `connected`
  * `disconnected`
  * `error`
* Terminal should show clear user-facing error messages.
* Terminal output should only render server `output` messages.
* Status and error messages should render in the UI, not as random terminal output unless intentionally mirrored.

Acceptance criteria:

* Switching devices does not create duplicate sessions.
* Rapid switching does not leave ghost WebSockets.
* Terminal reconnect works.
* `stty size` reflects browser terminal size.
* Bad password shows readable UI error.
* Offline host shows readable UI error.
* Password does not appear in WebSocket URL.
* `go test ./...` passes.
* `cd web && npm run build` passes.

## 1.2 WebSocket origin security

Current behavior must not remain permissive.

Tasks:

* Replace `CheckOrigin: return true`.
* Same-origin should be allowed by default.
* Local dev origins should be allowed in dev mode:

  * `http://localhost:5173`
  * `http://127.0.0.1:5173`
  * backend local origin
* Add config:

  * `SHELLWAVE_ALLOWED_ORIGINS`
* Reject unexpected origins.
* Add tests for origin checking if practical.

Acceptance criteria:

* Local dev still works.
* Same-origin production works.
* Random external origins cannot open terminal WebSockets.

## 1.3 Improve device connection testing

Current TCP-only testing is not enough.

Tasks:

* Keep TCP reachability test.
* Add SSH auth test.
* After auth succeeds, run:

```bash
echo shellwave-ok
```

Return a structured result:

```json
{
  "dnsOk": true,
  "portOpen": true,
  "sshAuthOk": true,
  "commandOk": true,
  "message": "Connected successfully"
}
```

Failure examples:

```json
{
  "dnsOk": true,
  "portOpen": true,
  "sshAuthOk": false,
  "commandOk": false,
  "message": "SSH authentication failed"
}
```

Acceptance criteria:

* Wrong password says auth failed.
* Closed port says port unreachable.
* Invalid host says DNS/host resolution failed.
* Valid SSH says connected.
* UI can show where connection failed.

---

# Phase 2 — SQLite migration

## Goal

Replace JSON persistence with SQLite before adding saved commands, history, known hosts, and settings.

## 2.1 Add SQLite store

Tasks:

* Add SQLite dependency.
* Create migration system.
* Create schema version table.
* Create `internal/store` implementation backed by SQLite.
* Preserve clean store API.
* Preserve `-data` flag.
* Default DB path:

```txt
~/.config/shellwave/shellwave.db
```

* If a JSON store exists, optionally support one-time migration into SQLite.

Acceptance criteria:

* App starts with empty SQLite DB.
* App creates required tables automatically.
* App survives restart with data intact.
* Tests cover migrations.

## 2.2 Initial schema

Create these tables:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

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
```

## 2.3 Data policy

Store:

* Device metadata.
* Auth mode.
* Key path.
* Known host fingerprints.
* Saved commands.
* Command history.
* Admin password hash.
* Settings.
* Log presets.
* Lightweight run previews if useful.

Do not store:

* Raw passwords.
* Private keys.
* SSH passphrases.
* Full terminal session output.
* Full command output by default.

Acceptance criteria:

* No plaintext password column exists.
* Saved commands survive restart.
* Command history survives restart.
* Known hosts survive restart.
* Store tests pass.

---

# Phase 3 — SSH auth and host trust

## Goal

Support selected auth modes safely and consistently.

Required auth modes:

* SSH agent.
* SSH key path.
* Password fallback.
* Tailscale SSH later, not required immediately.

## 3.1 Auth resolver

Ensure terminal sessions and command runner use the same auth resolver.

Tasks:

* Support `AuthConfig`:

```go
type AuthConfig struct {
    Type       string `json:"type,omitempty"` // password, key, agent
    Password   string `json:"password,omitempty"`
    KeyPath    string `json:"keyPath,omitempty"`
    Passphrase string `json:"passphrase,omitempty"`
    UseAgent   bool   `json:"useAgent,omitempty"`
}
```

* Auth mode behavior:

  * `agent`: use `SSH_AUTH_SOCK`.
  * `key`: load key from configured path.
  * `password`: use provided password for session only.
* Default Tailscale-imported devices to `agent`.
* Manual devices can select auth mode.
* Do not persist password.
* Do not persist passphrase.

Acceptance criteria:

* Terminal works with password auth.
* Terminal works with key path auth.
* Terminal works with SSH agent auth.
* Command runner works with the same auth modes.
* Auth failures are clear.
* Tests cover auth resolver behavior.

## 3.2 Known hosts / trust-on-first-use

Remove unsafe default host key behavior.

Tasks:

* Remove `ssh.InsecureIgnoreHostKey()` from normal production paths.
* Add known-host trust implementation.
* On unknown host:

  * block connection
  * return structured error
  * include host
  * include port
  * include key type
  * include SHA256 fingerprint
  * include public key
* Frontend must show a trust prompt.
* User can accept fingerprint.
* Fingerprint is saved in SQLite.
* Future connections verify fingerprint.
* Changed fingerprint blocks connection with a strong warning.

Suggested API:

```txt
POST /api/known-hosts/trust
GET  /api/known-hosts
DELETE /api/known-hosts/:id
```

Trust request:

```json
{
  "host": "server.tailnet.ts.net",
  "port": 22,
  "keyType": "ssh-ed25519",
  "fingerprintSha256": "...",
  "publicKey": "..."
}
```

Acceptance criteria:

* First connection asks for trust.
* Accepted host connects next time.
* Changed host key is blocked.
* No production connection path silently ignores host keys.
* Tests cover unknown host, trusted host, and changed host.

---

# Phase 4 — Device discovery and onboarding UI

## Goal

Make first-run onboarding excellent.

The backend already supports local Tailscale status/import. Build UI and polish around it.

## 4.1 Welcome/setup screen

When device list is empty, show a setup screen.

States:

* Tailscale installed and active.
* Tailscale installed but not logged in.
* Tailscale not installed.
* Tailscale CLI not on PATH.
* Manual-only mode.

Actions:

* Import tailnet devices.
* Add manual device.
* Configure default SSH user.
* Configure default auth mode.
* Explain where Shellwave stores data.

Acceptance criteria:

* Empty state is useful.
* User understands what to do next.
* User can continue without Tailscale.

## 4.2 Tailnet import UI

Tasks:

* Fetch `/api/tailscale/status`.
* Show discovered peers.
* Allow selecting all/some peers.
* Show:

  * hostname
  * MagicDNS
  * Tailscale IP
  * OS
  * online/offline
  * tags
* Allow setting default SSH user.
* Allow per-device user override.
* Import selected devices.

Acceptance criteria:

* User can import tailnet devices without typing IPs.
* Imported devices use source `tailscale`.
* Imported devices default to auth mode `agent`.
* User can edit user/port/auth later.

## 4.3 Device list

Tasks:

* Device search.
* Filters:

  * all
  * online
  * offline
  * tailnet
  * manual
  * favorites
* Badges:

  * tailnet/manual
  * auth mode
  * OS
  * online/offline
* Favorite pin.
* Edit device action.
* Test connection action.
* Delete manual device action.
* Hide/remove imported device later if needed.

Acceptance criteria:

* Device list is backend-driven.
* No hard-coded mock devices.
* User can add/edit/test devices.
* User can distinguish manual vs tailnet devices.

---

# Phase 5 — Device overview

## Goal

Make Shellwave useful before opening a terminal.

## 5.1 Backend overview probe

Expand overview probe to collect:

* hostname
* whoami
* OS
* kernel
* uptime
* load average
* CPU count
* memory used/total
* disk used/total for `/`
* Docker installed/running count
* Tailscale status if available
* listening ports
* primary IPs

Use defensive commands because not every machine has the same tooling:

```bash
hostname 2>/dev/null
whoami 2>/dev/null
uname -sr 2>/dev/null
uptime 2>/dev/null
cat /etc/os-release 2>/dev/null
df -h / 2>/dev/null
free -m 2>/dev/null
nproc 2>/dev/null
ss -tulpen 2>/dev/null || netstat -tulpen 2>/dev/null
command -v docker 2>/dev/null
docker ps --format json 2>/dev/null
command -v tailscale 2>/dev/null
tailscale status --json 2>/dev/null
```

Return structured JSON, not only raw command text.

Suggested shape:

```json
{
  "hostname": "server",
  "user": "drake",
  "os": "Ubuntu 24.04",
  "kernel": "Linux 6.x",
  "uptime": "up 3 days",
  "load": "0.12 0.08 0.04",
  "cpuCount": 4,
  "memory": {
    "usedMb": 1024,
    "totalMb": 4096,
    "percent": 25
  },
  "disk": {
    "mount": "/",
    "used": "20G",
    "total": "100G",
    "percent": "20%"
  },
  "docker": {
    "installed": true,
    "runningContainers": 3
  },
  "tailscale": {
    "installed": true,
    "available": true
  },
  "listeningPorts": []
}
```

Acceptance criteria:

* Overview probe works on common Linux machines.
* Missing commands degrade gracefully.
* Probe timeout does not hang UI.
* Errors are readable.

## 5.2 Frontend overview tab

Build Overview tab.

Sections:

* Device hero:

  * name
  * host
  * source
  * online/offline
  * auth mode
  * last seen
* Health cards:

  * uptime
  * memory
  * disk
  * load
  * Docker
  * ports
* Quick actions:

  * open terminal
  * run command
  * view logs
  * browse files
  * refresh overview

Acceptance criteria:

* Overview tab shows real remote data.
* Refresh overview works.
* Overview is useful even if terminal is not opened.
* Missing data shows clean fallback states.

---

# Phase 6 — Command palette and runbooks

## Goal

Make Shellwave keyboard-native and useful for repeated operations.

Required:

* `Cmd+K` command palette.
* Built-in command templates.
* Saved commands.
* Global and per-device commands.
* Dangerous command detection.
* Command history only, not full terminal output.

## 6.1 Command palette

Build global command palette.

Actions:

* Open terminal.
* Refresh overview.
* Run saved command.
* Create saved command.
* View logs.
* Open files.
* Open Docker.
* Open services.
* Open processes.
* Edit device.
* Add device.
* Import tailnet devices.

Acceptance criteria:

* `Cmd+K` opens palette.
* Search filters actions.
* Keyboard navigation works.
* Palette is useful with and without selected device.

## 6.2 Built-in command templates

Seed useful built-ins:

* System info.
* Disk usage.
* Memory usage.
* Top processes.
* Listening ports.
* Tailscale status.
* Docker ps.
* Docker logs.
* Journal logs.
* Restart service.
* Reboot.
* Update packages.

Template shape:

```ts
{
  id: string;
  name: string;
  description: string;
  command: string;
  category: string;
  dangerous: boolean;
  requiresConfirm: boolean;
  supportsStreaming: boolean;
  defaultTimeout: number;
}
```

## 6.3 Saved commands

Support:

* Global saved commands.
* Per-device saved commands.
* Categories.
* Description.
* Dangerous flag.
* Confirmation required.
* Last run timestamp.
* Run count.

## 6.4 Command history

Store only:

* command
* device ID
* exit code
* started at
* finished at
* duration
* source
* saved command ID if applicable

Do not store full output by default.

Acceptance criteria:

* User can run a built-in command.
* User can save custom command globally.
* User can save custom command per device.
* Dangerous commands require confirmation.
* Command history shows recent commands without full output.

---

# Phase 7 — Logs viewer

## Goal

Build a dedicated logs viewer. This is a core feature.

## 7.1 Backend log sources

Support these log sources:

* `journalctl`
* Docker logs
* Plain file tail
* Custom command log

Start non-streaming. Add follow/streaming mode later.

Commands:

```bash
journalctl -n 200 --no-pager
journalctl -u <service> -n 200 --no-pager
docker logs --tail 200 <container>
tail -n 200 <file>
```

## 7.2 Logs UI

Build Logs tab.

Features:

* Source picker.
* Search/filter text.
* Refresh button.
* Copy output.
* Save log preset.
* Choose:

  * system logs
  * service logs
  * Docker logs
  * file logs
  * custom command logs

Acceptance criteria:

* User can view recent system logs.
* User can view Docker logs if Docker exists.
* User can tail a plain file read-only.
* User can save common log sources.
* Errors are readable.

---

# Phase 8 — Files read-only

## Goal

Add read-only file browsing first. Do not add upload/delete/mkdir yet.

## 8.1 SFTP read-only backend

Add SFTP support.

Routes:

```txt
GET /api/devices/:id/files?path=
GET /api/devices/:id/files/download?path=
GET /api/devices/:id/files/preview?path=
```

Default starting path:

```txt
/home/<user>
```

Fallback:

```txt
~
```

Preview rules:

* Only preview text files.
* Limit preview size.
* Do not attempt binary preview.

## 8.2 Files UI

Build Files tab.

Features:

* Breadcrumb path.
* File/folder table.
* Name.
* Size.
* Modified date.
* Permissions.
* Download action.
* Preview for small text files.
* No mutation actions yet.

Acceptance criteria:

* User can browse directories.
* User can preview small text files.
* User can download files.
* No upload/delete/mkdir exists yet.
* Permission errors are readable.

---

# Phase 9 — Docker panel

## Goal

Add Docker management after command palette and overview.

Start with:

* list containers
* inspect container
* view logs
* restart/start/stop with confirmation

## 9.1 Backend Docker commands

Use remote Docker CLI:

```bash
docker ps --all --format json
docker inspect <container>
docker logs --tail 200 <container>
docker restart <container>
docker start <container>
docker stop <container>
```

All mutations require confirmation.

## 9.2 Docker UI

Build Docker tab.

Features:

* Containers table.
* Status badge.
* Image.
* Ports.
* Created.
* Actions.
* Logs panel.
* Inspect drawer.
* Missing Docker empty state.

Acceptance criteria:

* Docker tab detects missing Docker gracefully.
* Container list works.
* Logs work.
* Restart/start/stop require confirmation.
* Docker errors are readable.

---

# Phase 10 — Services and processes read-only

## Goal

Add read-only system inspection. No mutations yet.

## 10.1 Services

Backend commands:

```bash
systemctl list-units --type=service --state=running --no-pager
systemctl status <service> --no-pager
```

UI:

* Services table.
* Status.
* Description.
* View status.
* View logs.
* No restart/start/stop yet.

## 10.2 Processes

Backend commands:

```bash
ps aux --sort=-%mem | head
ps aux --sort=-%cpu | head
```

UI:

* Processes table.
* PID.
* User.
* CPU.
* Memory.
* Command.
* Search/filter.
* No kill yet.

Acceptance criteria:

* Services tab is read-only.
* Processes tab is read-only.
* Unsupported systems show helpful message.
* No destructive process/service actions exist yet.

---

# Phase 11 — App access/security

## Goal

Add single local admin password and session auth.

## 11.1 First-run admin setup

Tasks:

* On first run, detect no admin password.
* Frontend prompts user to create admin password.
* Store password hash in SQLite.
* Use strong password hashing:

  * argon2id preferred
  * bcrypt acceptable
* Do not store plaintext password.

## 11.2 Login/session

Tasks:

* Add login endpoint.
* Add logout endpoint.
* Use secure random session token.
* Store token hash in SQLite.
* Use HttpOnly cookie.
* Add session expiration.
* Protect APIs.
* Protect WebSocket terminal.
* Protect static app routes as appropriate.

Suggested routes:

```txt
GET  /api/auth/status
POST /api/auth/setup
POST /api/auth/login
POST /api/auth/logout
```

## 11.3 Security defaults

Tasks:

* Restrict WebSocket origins.
* Rate limit login attempts.
* Rate limit SSH connection attempts.
* Redact secrets in logs.
* Add host allowlist setting:

  * private/tailnet ranges by default
  * optional allow any
* Ensure unauthenticated user cannot use APIs.
* Ensure unauthenticated user cannot open terminal WebSocket.

Acceptance criteria:

* First run requires admin setup.
* Login required by default.
* API requires auth.
* WebSocket requires auth.
* Secrets are not logged.
* Rate limiting exists.
* Private/tailnet host allowlist exists.

---

# Phase 12 — UI redesign pass

## Goal

Make the app feel like a polished modern devtool.

Do this after major surfaces exist. Do not spend too much time polishing placeholders.

## 12.1 Layout

Use:

```txt
Top command/search bar
Left device sidebar
Main device workspace
Tabs:
  Overview
  Terminal
  Commands
  Logs
  Files
  Docker
  Services
  Processes
  Settings
```

## 12.2 Visual style

Use:

* Inter for UI.
* JetBrains Mono for terminal/code.
* Near-black background.
* Subtle violet/cyan gradients.
* Soft card borders.
* Rounded 2xl cards.
* Smooth hover/focus states.
* Sparse lime/cyan accent.
* Command palette feel.

Avoid:

* Full black/green hacker terminal aesthetic everywhere.
* Corporate enterprise admin dashboard look.
* Dense tables without context.
* Placeholder tabs.

## 12.3 Empty states

Every tab needs useful empty states:

* No device selected.
* No Tailscale detected.
* No Docker installed.
* No services available.
* No saved commands yet.
* No logs preset yet.
* SSH auth missing.
* Host not trusted yet.
* Files permission denied.
* Command failed.

Acceptance criteria:

* App feels polished.
* No placeholder “under development” panels remain.
* User always knows next action.
* Terminal is one feature among many, not the whole app.

---

# Phase 13 — Mobile/responsive polish

## Goal

Basic responsive support. Desktop remains primary, but mobile/tablet should not be broken.

Tasks:

* Collapsible device sidebar.
* Tablet-friendly layout.
* Command palette works on mobile.
* Drawer or bottom nav for small screens.
* Avoid tiny click targets.
* Terminal usable enough on phone/tablet.
* Overview/logs/files should work well on mobile.

Acceptance criteria:

* iPad/tablet is comfortable.
* Phone can browse devices, overview, logs, and files.
* Terminal is usable enough, but not the primary mobile workflow.

---

# Phase 14 — Testing and QA

## 14.1 Backend tests

Add/expand tests for:

* SQLite migrations.
* Store CRUD.
* Device CRUD.
* Tailscale parser.
* Tailscale import merge.
* WebSocket protocol.
* SSH auth resolver.
* Known-host trust flow.
* Command runner timeout.
* Dangerous command detection.
* Saved command CRUD.
* Log preset CRUD.
* App auth/session behavior.
* Origin checking.

## 14.2 Frontend tests

Add tests for:

* Device list rendering.
* Tailnet import flow.
* Add manual device flow.
* Edit device flow.
* Terminal state rendering.
* Command palette search.
* Saved command form.
* Dangerous command confirmation.
* Overview empty/error states.
* Login/setup flow.

## 14.3 Manual E2E checklist

Test against:

* Mac dev machine.
* Linux homelab server.
* Docker deployment.
* No Tailscale installed.
* Tailscale installed but logged out.
* Tailscale active.
* Wrong SSH password.
* SSH key auth.
* SSH agent auth.
* Unknown host key.
* Changed host key.
* Offline host.
* Docker missing.
* Systemd missing.
* Small screen.
* Rapid device switching.
* Large terminal output.
* Failed command.
* Dangerous command confirmation.

Acceptance criteria:

* `go test ./...` passes.
* `cd web && npm run build` passes.
* App does not silently fail.
* Every common failure has a readable error.
* Terminal remains stable under heavy output.

---

# Additional high-value features

These are not required immediately, but design the app so they can be added later.

## 1. Device command bar

A small input at the top:

```txt
Run on <device>...
```

It should autocomplete:

* saved commands
* built-ins
* recent commands
* device actions

## 2. Explain output helper

After running a command, add optional action:

```txt
Explain output
```

This can send the current visible output to an LLM only when clicked. Do not store full output permanently by default.

## 3. Health snapshots

Store lightweight snapshots whenever overview is refreshed:

* disk usage
* memory
* uptime
* Docker count
* last successful connection
* probe timestamp

Later this can become trend history.

## 4. Device profiles/groups

Allow grouping devices:

* Homelab
* Work
* Raspberry Pi
* Docker hosts
* Storage
* Macs
* Linux servers

## 5. First aid checks

One-click diagnostic bundle:

* Ping/check SSH.
* Disk usage.
* Memory.
* Load.
* Docker status.
* Tailscale status.
* Recent logs.
* Listening ports.

Output should become a clean diagnostic report.

## 6. Safe mode for destructive commands

For dangerous actions, require typing the device name before execution.

Examples:

* reboot
* shutdown
* docker stop
* docker restart
* systemctl restart
* package upgrade

## 7. Snippets with variables

Saved commands can support variables:

```bash
journalctl -u {{service}} -n {{lines}}
docker logs --tail {{lines}} {{container}}
```

The UI should render a small form before running.

## 8. Per-device notes

Add a notes panel:

* What this server does.
* Important paths.
* Common services.
* Recovery commands.
* Owner/context.

## 9. Recent failures panel

Track recent failed operations:

* device
* command/action
* exit code
* timestamp
* short error

Do not store secrets.

## 10. Shareable local runbooks

Export/import saved commands as JSON/YAML so users can share command packs.

---

# Immediate next implementation task

Start with this milestone:

## Milestone: Reliable Core + SQLite + SSH Trust

Implement the following before moving to overview/logs/files/Docker.

### Required tasks

1. Replace JSON store with SQLite.

   * Add migrations.
   * Store devices, settings, known_hosts, saved_commands, command_history, command_runs, log_presets, and sessions.
   * Do not store raw passwords.
   * Default DB path: `~/.config/shellwave/shellwave.db`.
   * Keep `-data` flag support.

2. Finish SSH auth consistency.

   * Terminal sessions and command runner must use same `AuthConfig` resolver.
   * Support password, key path, and SSH agent.
   * Default Tailscale imported devices to agent auth.
   * Manual devices can select auth mode.

3. Implement known-host verification.

   * Remove `ssh.InsecureIgnoreHostKey()` from normal production paths.
   * Add trust-on-first-use.
   * Unknown host key returns structured error with fingerprint.
   * Add API to accept/store host fingerprint.
   * Block changed fingerprints.

4. Harden WebSocket origin checking.

   * Same-origin by default.
   * Allow dev localhost.
   * Add optional allowed origins config.

5. Improve device testing.

   * Return structured result for DNS/host, port reachability, SSH auth, and command test.
   * Run `echo shellwave-ok` after auth succeeds.

6. Update frontend enough to support:

   * Auth mode selection on add/edit device.
   * Host trust prompt.
   * Improved connection/test errors.
   * Device list loaded from backend.
   * No mock devices.

7. Add/expand tests:

   * SQLite migrations.
   * Store CRUD.
   * Auth resolver.
   * Known host trust flow.
   * Device test response shape.
   * WebSocket protocol remains valid.

### Acceptance criteria

* `go test ./...` passes.
* `cd web && npm run build` passes.
* App can add a manual device.
* App can test SSH connection.
* Terminal connects using password, key path, or agent.
* Password does not appear in WebSocket URL.
* Unknown host key prompts for trust.
* Accepted host key is remembered.
* Changed host key is blocked.
* WebSocket origin is no longer open to all origins.
* No hard-coded mock devices remain.
* Existing Tailscale import still works.

---

# Non-goals for immediate milestone

Do not build these until Reliable Core + SQLite + SSH Trust is complete:

* Full command palette.
* Logs viewer.
* Files browser.
* Docker panel.
* Services/processes tabs.
* Full visual redesign.
* Remote agent.
* Tailscale API integration.
* Multi-user/team accounts.
* SaaS features.

---

# Definition of done for MVP

The MVP is done when a user can:

1. Start Shellwave.
2. Create admin password.
3. Import Tailscale devices or add manual devices.
4. Select a device.
5. Trust host key.
6. Connect with SSH agent, key path, or password fallback.
7. See real overview cards.
8. Open a reliable terminal.
9. Run built-in and saved commands.
10. View command history.
11. View logs.
12. Browse files read-only.
13. Inspect Docker containers.
14. View services/processes read-only.
15. Use the app without placeholder tabs.
16. Understand every major error state.
17. Restart the app without losing devices/settings/commands/history.

Build toward that.
