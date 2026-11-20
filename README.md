# proxier

Forward TCP and UDP ports to other servers, managed from a web UI. One static binary, no root required.

Typical use: a friend can't reach a game or voice server because of routing or geoblocking (Sweden → Russia fails, Germany → Russia works). Put proxier on a cheap VPS in Germany, add `UDP 9987 → ts.example.ru:9987`, and your friend connects to the VPS instead.

```
friend ──► vps.example.com:9987 ──(proxier)──► ts.example.ru:9987
```

## Features

- **TCP, UDP or both** per rule; single ports or **ranges** (`27000-27050 → host:27000-27050`, max 1000 ports per rule).
- **Presets** for TeamSpeak 3, Minecraft (Java/Bedrock), CS2/Source, Rust, Valheim, Factorio, Terraria and WireGuard. You can also save your own presets from existing rules.
- Targets can be **IPs or hostnames**, which are re-resolved every 60s, so dynamic DNS works. Dual-stack listening, with an optional per-rule bind IP.
- **Live view:**
  - throughput charts and active connections/UDP sessions
  - per-connection rates
  - **kick** or **ban** a client
- **History:** per-minute traffic for 7 days and hourly for a year (configurable), stored in SQLite.
- **Event log:** connections, bind errors, rule changes and logins, searchable.
- **IP/CIDR blocklist**, global or per rule, with optional expiry.
- **Port conflict detection:** catches overlaps with other rules, the UI's own port, and ports already used by other programs.
- Enable/disable rules without deleting them. Rules whose port fails to bind are retried every 30s.
- JSON **export/import** for backups or moving servers.
- Single admin with password login, argon2id hashing, login throttling and a one-time setup token on first run.
- Built-in HTTPS: off, self-signed, Let's Encrypt (ACME tls-alpn-01) or your own cert files, which reload automatically.

## Install

### Linux server (recommended)

The install script downloads the latest release for your CPU (amd64, arm64 or armv7) and verifies its checksum. It then sets proxier up as a systemd service: a `proxier` system user, data in `/var/lib/proxier`, config in `/etc/proxier/proxier.env`.

```sh
curl -fsSL https://github.com/ilbinek/proxier/releases/latest/download/install.sh | sudo sh

# with options (any `proxier install` flag works after --):
curl -fsSL https://github.com/ilbinek/proxier/releases/latest/download/install.sh | sudo sh -s -- --listen :8443 --tls self-signed
```

Then get the one-time setup token:

```sh
sudo cat /var/lib/proxier/setup-token.txt     # or: journalctl -u proxier | grep -i "setup token"
```

Open `https://<vps-ip>:8080` (or the port you chose), accept the self-signed certificate, enter the setup token and choose an admin password.

Open the UI port and the ports you forward in your firewall, both on the VPS (`ufw allow 8080/tcp`, `ufw allow 9987/udp`, …) and in your hosting provider's panel.

- **Upgrade:** run the same command again. The binary is replaced and the service restarted; config and data are kept.
- **Specific version:** `curl -fsSL … | sudo PROXIER_VERSION=v1.2.0 sh`
- **Uninstall:** `sudo proxier uninstall` (add `--purge` to also delete data, config and the user).

### Linux, manual

```sh
curl -LO https://github.com/ilbinek/proxier/releases/latest/download/proxier_<version>_linux_amd64.tar.gz
tar xzf proxier_*_linux_amd64.tar.gz
sudo ./proxier install --listen :8443 --tls self-signed
```

Archives are published for `linux_amd64`, `linux_arm64` and `linux_armv7` (Raspberry Pi), with a `checksums.txt`.

### Docker

```sh
docker run -d --name proxier --network host --restart unless-stopped \
  -v proxier-data:/data -e PROXIER_TLS=self-signed \
  ghcr.io/ilbinek/proxier:latest
docker logs proxier 2>&1 | grep -i "setup token"
```

Or use [`docker-compose.yml`](docker-compose.yml). See [Docker](#docker) for why host networking is needed.

### Windows and macOS

Download `proxier_<version>_windows_amd64.zip` or `proxier_<version>_darwin_arm64.tar.gz` (or the other architecture) from the [releases page](../../releases), extract it, and run:

```sh
proxier serve --listen 127.0.0.1:8080      # Windows: .\proxier.exe serve --listen 127.0.0.1:8080
```

The setup token is printed in the terminal and written to `data/setup-token.txt`. The `install` command (systemd service) is Linux-only. On Windows, allow proxier through the firewall prompt for the ports you forward.

### From source

Requires Go 1.27+, Node 22+, pnpm and [Task](https://taskfile.dev/installation/).

```sh
git clone https://github.com/ilbinek/proxier && cd proxier
task build          # ./proxier (or proxier.exe) for this machine
task build:linux    # or cross-compile: bin/proxier_linux_amd64, _arm64, _armv7
```

### TeamSpeak 3 walkthrough

1. **Presets → TeamSpeak 3 → Use preset**, enter the TS3 server's address (e.g. `ts.example.ru`), then **Create rules**. This creates `UDP 9987` (voice) and `TCP 30033` (file transfer). ServerQuery is optional.
2. Your friends connect their TS3 client to `<vps-ip>` (or `<vps-ip>:9987`).
3. **Ask the TS3 admin to add the VPS IP to `query_ip_allowlist.txt`.** Everyone behind the proxy shares that IP, so TS3's anti-flood protection could otherwise ban the proxy and kick everyone at once. The rule page has a ready-to-copy message for the admin.
4. Keep the file-transfer port the same on both sides (30033 → 30033). The TS3 server tells clients which port to use.

## Configuration

proxier's own settings come from flags or environment variables, never from the database, so a mistake in the UI can't lock you out. With systemd they live in `/etc/proxier/proxier.env`; restart the service after changing them.

| Flag | Env | Default | |
|---|---|---|---|
| `--data-dir` | `PROXIER_DATA_DIR` | `./data` | SQLite DB, certificates, setup token |
| `--listen` | `PROXIER_LISTEN` | `:8080` | Web UI address, e.g. `127.0.0.1:8080` for SSH-tunnel-only access |
| `--tls` | `PROXIER_TLS` | `off` | `off`, `self-signed`, `acme`, `files` |
| `--acme-domain` | `PROXIER_ACME_DOMAIN` | | Domain for Let's Encrypt. The UI must be reachable on `:443` |
| `--acme-email` | `PROXIER_ACME_EMAIL` | | Contact email for Let's Encrypt |
| `--tls-cert`, `--tls-key` | `PROXIER_TLS_CERT`, `PROXIER_TLS_KEY` | | PEM files for `--tls=files` (reloaded when they change) |
| `--trust-proxy` | `PROXIER_TRUST_PROXY` | `false` | Trust `X-Forwarded-For/Proto` when behind your own nginx/Caddy |
| `--log-level` | `PROXIER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Ways to expose the UI:

- **Public with HTTPS:** `--listen :443 --tls acme --acme-domain proxy.example.com`, or `--tls self-signed` on any port.
- **Behind a reverse proxy:** `--listen 127.0.0.1:8080 --trust-proxy`, with nginx or Caddy terminating TLS. WebSockets must be proxied for `/api/ws`.
- **SSH tunnel only:** `--listen 127.0.0.1:8080`, then `ssh -L 8080:127.0.0.1:8080 vps` and open `http://localhost:8080`.

UI settings (public address shown in "connect to" hints, retention periods) are stored in the database.

### Commands

```
proxier [serve] [flags]          run the server
proxier install [flags]          install systemd service (Linux, root)
proxier uninstall [--purge]      remove it (--purge also deletes data and the user)
proxier reset-password [--password X] [--data-dir D]   new admin password; logs out all sessions
proxier version
```

## Docker

```sh
docker compose up -d
docker compose logs proxier | grep -i "setup token"
```

The compose file uses `network_mode: host`. Rules are created at runtime from the UI, so their ports can't be pre-declared in a `ports:` section. The image runs as a non-root user, so to forward ports below 1024, set `user: "0"`.

## Security notes

- First run requires the **setup token** from the server log or `setup-token.txt`, so a freshly exposed instance can't be claimed by someone else.
- The session cookie is `HttpOnly` and `SameSite=Strict` (and `Secure` over HTTPS). State-changing API calls require an `X-Proxier` header, and WebSockets are origin-checked.
- After 5 failed logins an IP is locked out, starting at 30s and doubling up to 15 minutes. Failed logins show up in Events.
- The systemd unit runs as an unprivileged user with `CAP_NET_BIND_SERVICE` and a hardened sandbox (`ProtectSystem=strict`, `NoNewPrivileges`, …).
- Forwarded ports are open to anyone. Use bans to block abusers. The target server sees the VPS's IP, not the player's.

## Development

Requirements: Go 1.27+, Node 22+, pnpm and [Task](https://taskfile.dev/installation/) (`winget install Task.Task`, `brew install go-task`, or `go install github.com/go-task/task/v3/cmd/task@latest`). The Taskfile works the same on Windows, Linux and macOS.

```sh
task                 # list all tasks
task dev             # Go server on 127.0.0.1:8080 + Vite with hot reload on http://localhost:5173
task run             # build everything and run the real binary on 127.0.0.1:8080 (data in ./data)
task run -- --tls self-signed      # extra serve flags go after --

task build           # UI + binary for this machine: ./proxier or ./proxier.exe
task build:linux     # bin/proxier_linux_{amd64,arm64,armv7}
task build:windows   # bin/proxier_windows_{amd64,arm64}.exe
task build:darwin    # bin/proxier_darwin_{amd64,arm64}
task build:all       # all of the above
task build:target TARGET=freebsd/amd64   # any other GOOS/GOARCH

task test            # go test ./...
task test:race       # with the race detector (needs a C compiler)
task lint            # go vet, gofmt, tsc, eslint
task check           # lint + test, same as CI
task fmt             # gofmt -w

task docker          # docker image proxier:<version>
task release:snapshot  # full release build with GoReleaser into dist/ (no publishing)
task clean           # remove build outputs (keeps ./data)
```

The UI is only rebuilt when files under `web/` change, so repeated `task build`/`task run` are fast.

### CI/CD

GitHub Actions (`.github/workflows/`):

- **CI** runs on every push to `master` and on pull requests:
  - lint
  - tests on Linux (with `-race`), Windows and macOS
  - a GoReleaser snapshot build of every release archive, downloadable from the run's **Artifacts**
  - a multi-arch Docker build that isn't pushed
- **Release** runs when you push a `v*` tag. It tests again, then:
  - publishes a GitHub release with archives for Linux (amd64/arm64/armv7), Windows and macOS (amd64/arm64), plus `checksums.txt` and `install.sh`
  - pushes a multi-arch image to `ghcr.io/ilbinek/proxier` tagged `1.2.3`, `1.2` and `latest`

```sh
git tag v1.0.0
git push origin v1.0.0
```

Tags with a suffix (`v1.1.0-rc.1`) become pre-releases and don't move `latest`. The release uses the built-in `GITHUB_TOKEN`, so no secrets need to be configured. After the first image push, the GHCR package is private by default. To allow anonymous `docker pull`, make it public under the repository's **Packages** settings.

### Layout

```
cmd/proxier/        entrypoint and subcommands
internal/proxy/     forwarding engine: TCP/UDP listeners, sessions, bans, resolver, port checks
internal/app/       business logic: validation, conflict checks, presets, import/export
internal/api/       REST API, auth middleware, WebSocket hub
internal/store/     SQLite (modernc.org/sqlite, no CGO) and migrations
internal/stats/     1s live sampling, per-minute persistence, hourly rollups and retention
internal/events/    event log persistence and fan-out
internal/auth/      argon2id, sessions, setup token, throttling
internal/tlsx/      self-signed, ACME, cert-file TLS
internal/install/   systemd installer
web/                React 19 + TypeScript + Tailwind v4 + shadcn/ui, embedded via go:embed
```

### How forwarding works

- **TCP:** each accepted connection is checked against the bans, then a connection is dialled to the target and bytes are copied both ways. Half-close is propagated.
- **UDP:** each client address gets its own upstream socket, so the target sees a distinct source port per player, which TS3 needs. Sessions close after the rule's idle timeout (default 120s).
- Changing a rule's target closes its existing connections so clients reconnect to the new target. Disabling or deleting a rule frees the port immediately.
