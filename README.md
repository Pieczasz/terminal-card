# Terminal Card

[![Go Version](https://img.shields.io/github/go-mod/go-version/Pieczasz/terminal-card)](https://go.dev/)
[![License](https://img.shields.io/github/license/Pieczasz/terminal-card)](./LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/Pieczasz/terminal-card/test.yml?branch=main)](https://github.com/Pieczasz/terminal-card/actions)

Terminal Card is an SSH server for multiplayer card games. Built with Go and the [Charm](https://charm.sh/) ecosystem (Bubble Tea, Wish), it gives players a rich TUI over plain SSH.

**v0.1** ships **Crazy Eights** and **No-Limit Texas Hold'em** (one ranked hand per match; rematch via lobby Ready). Chips are numeric stacks.

## Features

- **SSH multiplayer** – connect with any SSH client
- **Rich TUI** - Bubble Tea + Lip Gloss
- **Crazy Eights & Poker (NLHE)** – up to nine seats at Hold'em
- **Persistent stats** – PostgreSQL users, matches, and ELO
- **Observability** – OpenTelemetry logs, metrics, and traces into a built-in Grafana + Loki/Tempo/Prometheus stack
- **Pluggable games** - register a `game.Module` + TUI view factory

## Quick start (Docker Compose)

Requirements: Docker, Docker Compose, and an SSH client.

```bash
cp .env.example .env
# set a strong DB_PASSWORD in .env

docker compose up -d --build
```

Migrations run automatically via the `migrate` service before the backend starts. SSH host keys persist in the `ssh-keys` volume.

Connect (nginx proxies port 22 -> the backend):

```bash
ssh -p 22 yourname@localhost
```

The first connection with a given public key registers that username. Key fingerprint authenticates later connections.

### Observability

The LGTM stack (Alloy -> Loki/Tempo/Prometheus + Grafana) starts by default with `docker compose up`. Grafana is bound to `127.0.0.1:3000` with anonymous admin - reach it via an SSH tunnel (`ssh -L 3000:localhost:3000 your-host`) and keep ports 3000/9090/3200/3100 off the public interface. The monitoring services carry `mem_limit`s so they can't OOM the host.

## Self-hosting (Hetzner / AWS VM)

1. Provision a VM with Docker (**>=4 GB RAM** recommended for game + DB + LGTM).
2. Move host `sshd` off port 22 (e.g. `Port 2222` in `/etc/ssh/sshd_config`) and reconnect on that port - the game proxy will own 22. Or map another host port to the proxy in `compose.yaml`.
3. Firewall: allow TCP for the game SSH port (and your admin SSH port from trusted IPs). Do **not** open Postgres, Grafana, or backend `6969`.
4. Clone the repo, `cp .env.example .env`, set a strong unique `DB_PASSWORD`. Compose sets `ENV=production` on the backend.
5. `docker compose up -d --build`.
6. Connect: `ssh yourname@your-host` (or `-p <mapped-port>`).
7. Optional: cron backups - install `zstd`, then schedule `./scripts/backup.sh` (see script header). Protect the `backups/` directory.
8. Smoke-test: register with a new key -> create a lobby -> play Crazy Eights and Poker -> disconnect mid-hand -> reconnect -> check profile/leaderboard.

Notes:

- **Backend `:6969` must stay Docker-internal.** nginx speaks PROXY protocol to it; publishing `6969` (or running the binary bare on a public interface) lets clients spoof client IPs and weaken per-IP rate limits. Compose does not publish that port – keep it that way.
- Host keys live in the `ssh-keys` volume - keep that volume across redeployments so clients do not see host-key changes.
- Grafana is `127.0.0.1:3000` with anonymous admin - tunnel only (`ssh -L 3000:localhost:3000 user@host`). Never publish obs ports.
- New lobbies default to **casual**; toggle Mode to Ranked in the create/lobby UI when you want Elo.
- Open SSH-key registration is intentional for public demos. Use a firewall/VPN/allowlist for a private community – see [SECURITY.md](SECURITY.md).
- Compose sets `DB_SSLMODE=disable` for the internal Postgres network. For an external managed DB, set `DB_SSLMODE=require` and supply CA-trusted TLS.

## Local development

```bash
# prerequisites: Go 1.26+, PostgreSQL, golangci-lint (optional)

cp .env.example .env
# start Postgres, then:
export DB_DSN='postgres://postgres:PASSWORD@localhost:5432/terminal_card?sslmode=disable'
make migrate-up
make test-short
make build
./bin/server
```

Useful Make targets:

| Target                                | Purpose                             |
|---------------------------------------|-------------------------------------|
| `make test-short`                     | Unit tests (no Docker)              |
| `make test` / `make test-integration` | Full suite including testcontainers |
| `make lint`                           | golangci-lint                       |
| `make build`                          | Build `bin/server`                  |

## Architecture

```text
SSH client -> nginx (optional) -> Wish SSH server -> Bubble Tea TUI
                                      ↓
                              game.Registry / Engine
                                      ↓
                              PostgreSQL (users, matches, ELO)
```

- Game logic implements `game.Rules`; rules and TUI view are registered together as one entry in `internal/catalog`.
- `cmd/server/main.go` and `internal/tui/app.go` both read `catalog.All`, so a game cannot be registered without a view.
- Lobby create/join and routing use the registry – no hardcoded game lists.

## Configuration

| Variable                      | Default                       | Notes                                       |
|-------------------------------|-------------------------------|---------------------------------------------|
| `ENV`                         | `development`                 | `production` requires `DB_PASSWORD`         |
| `SERVER_HOST`                 | `0.0.0.0`                     | Listen address                              |
| `SERVER_PORT`                 | `6969`                        | Backend SSH port (compose uses nginx :22)   |
| `MAX_CONNECTIONS`             | `1000`                        | Concurrent TCP connections                  |
| `SSH_KEY_PATH`                | `.wishlist/server`            | Host key path                               |
| `DB_*`                        | see `.env.example`            | Postgres connection                         |
| `DB_SSLMODE`                  | `disable` / `require` in prod |                                             |
| `DB_MAX_OPEN_CONNS`           | `25`                          | Postgres pool size (independent of SSH max) |
| `RATE_LIMIT_CONNECTIONS`      | `5`                           | Per-IP SSH connection budget                |
| `RATE_LIMIT_WINDOW_MS`        | `1000`                        | Sliding window                              |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`              | OTLP logs, metrics, traces                  |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` in dev                 | Set `false` for TLS                         |
| `SERVICE_VERSION`             | `0.1.0`                       | Reported in OTel resource                   |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, coding standards, and how to add a new card game.

## License

MIT - see [LICENSE](LICENSE).
