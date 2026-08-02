# Security Policy

## Supported versions

Security fixes are applied to the latest `main` branch and tagged releases starting at v0.1.0.

## Auth model

Terminal Card identifies players by **SSH public key fingerprint**:

- `wish.WithPublicKeyAuth` accepts any public key.
- The first successful connection with a new fingerprint registers the SSH username (subject to username validation).
- Later connections reuse the same fingerprint -> the same account.
- Fingerprints are stored with a **UNIQUE** constraint.

Implications:

- Usernames are first-come-first-served for a given deployment.
- Anyone who can reach the SSH port can register a new account with a new key.
- New lobbies default to **casual** (no Elo). Leaders can opt into ranked via the create/lobby Mode setting (`WithRanked(true)`).

## Rate limiting

Incoming SSH sessions are limited per client IP (`RATE_LIMIT_CONNECTIONS` / `RATE_LIMIT_WINDOW_MS`). When terminating SSH behind nginx stream proxy, prefer edge `limit_conn` (see `internal/config/nginx.conf`) because the proxy hop collapses client IPs unless PROXY protocol is enabled.

Lobby joins by code are rate-limited per player ID. Private lobby codes are eight characters (`A-Z0-9`).

Only one active session per user ID is allowed.

## Game fairness

Deck shuffling and first-player selection use `crypto/rand`. Mid-game reshuffles fail closed (forced pass / leave without trusting an unshuffled stock) if `crypto/rand` errors.

## PROXY protocol

The backend wraps its listener in PROXY protocol so nginx can forward the real client IP. The default policy **trusts the header from any peer**.

## Reporting a vulnerability

Please open a private security advisory on GitHub (or email the maintainer listed on the repository) with:

- A description of the issue and impact
- Steps to reproduce
- Affected version / commit if known

Do not file public issues for undisclosed vulnerabilities.

## Hardening checklist for public hosts (Hetzner / VPS)

- [ ] Strong unique `DB_PASSWORD` in `.env` (never commit `.env`)
- [ ] Move host `sshd` off port 22 (e.g. `Port 2222`) **before** `docker compose up`, or remap the game proxy to another host port
- [ ] Firewall / security group: allow game SSH only (22 or your mapped port); allow admin SSH on the relocated port from your IP if needed
- [ ] Never publish backend `:6969`, Postgres `:5432`, or observability ports publicly
- [ ] Persist the `ssh-keys` volume across redeployments
- [ ] Keep Grafana/Loki/Tempo/Prometheus/Alloy bound to localhost; reach Grafana via an SSH tunnel (`ssh -L 3000:localhost:3000 user@host`)
- [ ] Do not change Grafana bind to `0.0.0.0` while anonymous Admin is enabled; disable anonymous admin before any public Grafana exposure
- [ ] Cron Postgres backups (`./scripts/backup.sh`); protect `backups/` (may contain usernames and match history)
- [ ] Keep the host and container images updated
- [ ] Prefer `DB_SSLMODE=require` for databases outside the composition network
- [ ] Only set `ALLOW_INSECURE_DB=true` for trusted internal networks (compose sets this for the internal `db` service)
- [ ] Smoke-test after deployment: register -> create a lobby -> play -> disconnect mid-game -> reconnect -> leaderboard