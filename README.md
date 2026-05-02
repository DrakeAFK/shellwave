# ShellWave

ShellWave is a self-hosted server console. It's a Go backend and Svelte frontend that gives you a terminal in your browser, imports your Tailscale machines, and stores metadata locally in SQLite. 

> [!WARNING]
> **Alpha Status**: This is an early preview. The core terminal, Tailscale import, and SSH auth work, but it is not yet feature complete. The API and data model will change. Expect bugs. 
> 
> Currently working: Admin login, manual devices, Tailscale CLI import, SSH terminal, command execution, basic overview probe, known-host trust.
> Not yet implemented: Log viewer, file browser, Docker panel, services/processes panels, saved commands.

## Getting Started

### Docker (Recommended)

The easiest way to run ShellWave is with Docker Compose. This ensures data persistence and a safe, non-root runtime.

```sh
docker-compose up -d
```

By default, the `docker-compose.yml` binds to `127.0.0.1:4000`. If you are putting this behind a reverse proxy (like Caddy, Nginx, or Tailscale Serve), you can adjust the port mapping.

### Local Development

1. Run the backend: `make dev-server`
2. Run the frontend in another terminal: `make dev-web`
3. Open `http://localhost:5173`. 

The Go server listens on `127.0.0.1:4000` by default.

## Security & Guardrails

ShellWave is effectively a browser-accessible SSH bridge. We take this seriously. 

* **Admin Login**: On first run, you will be prompted to create an admin password (stored as a bcrypt hash).
* **SSH Auth**: Passwords and key passphrases are sent only in HTTPS request bodies or the first WebSocket connect message. They are never in URLs and are never persisted by the backend.
* **Host Trust**: SSH host keys are verified with trust-on-first-use (TOFU). Unknown hosts prompt you to verify the fingerprint. Changed host keys are blocked.
* **Rate Limiting**: Login and SSH connection attempts are strictly rate-limited.
* **WebSocket Origins**: Same-origin requests are required.

### Host Allowlist (Important)

By default, ShellWave **blocks connections to public internet IPs**. It will only connect to private networks (RFC1918) or loopback addresses. This prevents your instance from being used as an open SSH proxy if exposed.

To allow extra internal hosts (e.g., specific domains or custom VPN ranges):
```sh
SHELLWAVE_HOST_ALLOWLIST_EXTRA=10.0.0.0/8,example.local
```

To fully disable this protection and allow connecting to any public host:
```sh
SHELLWAVE_ALLOW_PUBLIC_HOSTS=true
```

## Data & Backups

ShellWave stores all data in a single SQLite database at `/data/shellwave.db` (in Docker) or `$XDG_CONFIG_HOME/shellwave/shellwave.db` (locally).

**To backup your data**, just copy the `.db` file:
```sh
cp ./data/shellwave.db ./shellwave-backup.db
```

**To reset your installation** (including your admin password), simply delete the database file and restart the server.

## Troubleshooting

- **Tailscale Import Fails**: The Go backend needs access to the `tailscale` CLI binary. If running in Docker, you may need to mount the tailscale socket or use the manual device addition instead.
- **WebSocket Disconnects instantly**: If you are using a reverse proxy (Nginx, Traefik, Caddy), ensure WebSocket upgrade headers are properly forwarded.
- **SSH Auth Fails**: If using SSH agent forwarding, ensure your agent is running and accessible to the backend process. 
- **Docker Compose Permission Denied**: The Docker container runs as a non-root user (`appuser`). Ensure the `./data` directory is writable by the container.

## HTTPS / TLS

Do not expose ShellWave to the public internet without HTTPS. 

If you aren't using a reverse proxy, ShellWave can terminate TLS natively:
```sh
SHELLWAVE_TLS_CERT=/path/fullchain.pem SHELLWAVE_TLS_KEY=/path/privkey.pem ./server -addr :4000
```
When served over `https://`, the terminal automatically upgrades to `wss://`.
