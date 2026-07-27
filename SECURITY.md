# Security Policy

## Supported versions

Security fixes are applied to the latest `main` branch and tagged releases starting at v0.1.0.

## Auth model

Terminal Card identifies players by **SSH public key fingerprint**:

- `wish.WithPublicKeyAuth` accepts any public key (no allowlist by default).
- The first successful connection with a new fingerprint registers the SSH username (subject to username validation).
- Later connections reuse the same fingerprint -> same account.
- Fingerprints are stored with a **UNIQUE** constraint.

Implications:

- Usernames are first-come-first-served for a given deployment.
- Anyone who can reach the SSH port can register a new account with a new key.
- Protect the service with network controls (firewall, VPN, fail2ban, or an allowlist) if you need a private community.
- New lobbies default to **ranked**. Prefer casual lobbies (`WithRanked(false)` / UI when available) or network ACLs if open registration would enable Elo farming.



## Rate limiting

Incoming SSH sessions are limited per client IP (`RATE_LIMIT_CONNECTIONS` / `RATE_LIMIT_WINDOW_MS`). When terminating SSH behind nginx stream proxy, prefer edge `limit_conn` (see `internal/config/nginx.conf`) because the proxy hop collapses client IPs unless PROXY protocol is enabled.

Lobby joins by code are rate-limited per player ID. Private lobby codes are 8 characters (`A-Z0-9`).

Only one active session per user ID is allowed.

## Game fairness

Deck shuffling and first-player selection use `crypto/rand`.

## Reporting a vulnerability

Please open a private security advisory on GitHub (or email the maintainer listed on the repository) with:

- A description of the issue and impact
- Steps to reproduce
- Affected version / commit if known

Do not file public issues for undisclosed vulnerabilities.

## Hardening checklist for public hosts

- [ ] Strong unique `DB_PASSWORD`
- [ ] Persist the SSH host key volume across redeploys
- [ ] Do not expose Grafana/Loki/Alloy publicly (compose binds them to localhost; observability profile is optional)
- [ ] Disable Grafana anonymous admin before any public exposure
- [ ] Restrict SSH ingress (security group / firewall) when possible
- [ ] Keep the host and container images updated
- [ ] Prefer `DB_SSLMODE=require` for databases outside the compose network
- [ ] Only set `ALLOW_INSECURE_DB=true` for trusted internal networks (compose sets this for the internal `db` service)