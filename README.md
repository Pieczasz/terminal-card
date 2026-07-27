# Terminal Card

[![Go Version](https://img.shields.io/github/go-mod/go-version/Pieczasz/terminal-card)](https://go.dev/)
[![License](https://img.shields.io/github/license/Pieczasz/terminal-card)](./LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/Pieczasz/terminal-card/test.yml?branch=main)](https://github.com/Pieczasz/terminal-card/actions)

Terminal Card is an SSH server for multiplayer card games. Built with Go and the [Charm](https://charm.sh/) ecosystem (Bubble Tea, Wish), it gives players a rich TUI over plain SSH — no custom client install.

**v0.1** ships **Crazy Eights** and **No-Limit Texas Hold'em** (one ranked hand per match; rematch via lobby Ready). Chips are numeric stacks.

## Features

- **SSH multiplayer** — connect with any SSH client
- **Rich TUI** — Bubble Tea + Lip Gloss
- **Crazy Eights & Poker (NLHE)** — up to 9 seats at Hold'em
- **Persistent stats** — PostgreSQL users, matches, and ELO
- **Observability** — OpenTelemetry logs (optional Grafana/Loki/Alloy stack)
- **Pluggable games** — register a `game.Module` + TUI view factory

## Quick start (Docker Compose)

Requirements: Docker, Docker Compose, and an SSH client.

```bash
cp .env.example .env
# set a strong DB_PASSWORD in .env

docker compose up -d --build
```

Migrations run automatically via the `migrate` service before the backend starts. SSH host keys persist in the `ssh-keys` volume.

Connect (nginx proxies port 22 → the backend):

```bash
ssh -p 22 yourname@localhost
```

The first connection with a given public key registers that username. Later connections authenticate by key fingerprint.

### Observability

The LGTM stack (Alloy → Loki/Tempo/Prometheus + Grafana) starts by default with `docker compose up`. Grafana is bound to `127.0.0.1:3000` with anonymous admin — reach it via an SSH tunnel (`ssh -L 3000:localhost:3000 your-host`) and keep ports 3000/9090/3200/3100 off the public interface. The monitoring services carry `mem_limit`s so they can't OOM the host.

## Self-hosting (Hetzner / AWS VM)

1. Provision a VM with Docker and open TCP **22** (or map another host port to the proxy).
2. Clone the repo, copy `.env.example` → `.env`, set `DB_PASSWORD`, and set `ENV=production`.
3. Run `docker compose up -d --build`.
4. Point DNS (optional) and connect: `ssh yourname@your-host`.

Notes:

- **Port 22 collides with the host's own `sshd`.** Move the admin daemon to another port (e.g. `Port 2222` in `/etc/ssh/sshd_config`, then reconnect there) *before* `docker compose up`, or map a different host port to the proxy. The game owns 22.
- Host keys live in the `ssh-keys` volume — keep that volume across redeploys so clients do not see host-key changes.
- **Back up Postgres.** `db-data` holds all ELO/match history. Run `./scripts/backup.sh` on a cron (see the script header for the line and restore command).
- The full stack (game + DB + LGTM) wants **>=4 GB RAM**.
- Compose sets `DB_SSLMODE=disable` for the internal Postgres network. For an external managed DB, set `DB_SSLMODE=require` and supply CA-trusted TLS.
- See [SECURITY.md](SECURITY.md) for the auth model and hardening tips.

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

| Target | Purpose |
|--------|---------|
| `make test-short` | Unit tests (no Docker) |
| `make test` / `make test-integration` | Full suite including testcontainers |
| `make lint` | golangci-lint |
| `make build` | Build `bin/server` |

## Architecture

```text
SSH client → nginx (optional) → Wish SSH server → Bubble Tea TUI
                                      ↓
                              game.Registry / Engine
                                      ↓
                              PostgreSQL (users, matches, ELO)
```

- Game logic implements `game.Rules` and registers via `game.Module` in `cmd/server/main.go`.
- TUI views register by module slug in `internal/tui/app.go` (`gameViews` map).
- Lobby create/join and routing use the registry — no hardcoded game lists.

## Configuration

| Variable | Default | Notes |
|----------|---------|-------|
| `ENV` | `development` | `production` requires `DB_PASSWORD` |
| `SERVER_HOST` | `0.0.0.0` | Listen address |
| `SERVER_PORT` | `6969` | Backend SSH port (compose uses nginx :22) |
| `MAX_CONNECTIONS` | `1000` | Concurrent TCP connections |
| `SSH_KEY_PATH` | `.wishlist/server` | Host key path |
| `DB_*` | see `.env.example` | Postgres connection |
| `DB_SSLMODE` | `disable` / `require` in prod | |
| `DB_MAX_OPEN_CONNS` | `25` | Postgres pool size (independent of SSH max) |
| `RATE_LIMIT_CONNECTIONS` | `5` | Per-IP SSH connection budget |
| `RATE_LIMIT_WINDOW_MS` | `1000` | Sliding window |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP logs |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` in dev | Set `false` for TLS |
| `SERVICE_VERSION` | `0.1.0` | Reported in OTel resource |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, coding standards, and how to add a new card game.

## License

MIT — see [LICENSE](LICENSE).
