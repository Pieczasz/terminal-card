# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- Lobby join/create are atomic; mid-game joins rejected; host actions require leader actor
- Manager.Kick cleans `playerLobby`; join-by-code rate-limited; lobby codes are 8 chars
- Deck shuffle and first-player selection use `crypto/rand`
- Elo ratings clamped to `[100, 4000]` before `uint32` storage
- Registration errors sanitized for SSH clients; Wish uses structured session logging
- Hands cloned for TUI; game logs no longer dump player hands
- Broadcaster rejects subscribe-after-close; engines closed on lobby teardown
- Host key permissions re-checked on load; `.dockerignore` excludes `.env`
- Production rejects remote `DB_SSLMODE=disable` unless `ALLOW_INSECURE_DB=true`
- Go toolchain bumped to 1.26.5 (crypto/tls ECH fix)

### Fixed

- Ranked match ELO + history now commit in one transaction (`FinalizeRankedMatch`)
- Lobby getters lock shared state; creating a lobby requires a card game
- User+key registration is transactional; duplicate fingerprint rejected cleanly
- Broadcaster subscribers unsubscribed on TUI leave; engine broadcaster closed after games
- Rate limiter filters per-IP on the hot path (periodic full sweep)
- DB pool size decoupled from SSH max connections (`DB_MAX_OPEN_CONNS`)
- OTLP TLS controllable via `OTEL_EXPORTER_OTLP_INSECURE`; service version from env/build
- Post-condition failures finish the game instead of leaving torn playable state
- Ready/start re-checks readiness under the lobby lock
- SQL DB closed on graceful shutdown; MigrateDSN URL-encodes credentials

## [0.1.0] - 2026-07-12

### Added

- SSH multiplayer server with Bubble Tea TUI
- Crazy Eights as the first playable game
- PostgreSQL persistence for users, matches, and ELO
- `game.Module` registry for plug-in game registration and TUI routing
- Docker Compose deploy path with migrate job and persistent SSH host keys
- Sliding-window SSH rate limiting (configurable)
- OpenTelemetry log export (optional observability Compose profile)
- Self-host documentation and security policy

### Fixed

- Engine winner taken from standings instead of the acting player
- Action broadcasts only after post-conditions succeed
- `MultipleStandardDecks` no longer prepends zero-value cards
- Discard peek renamed to `Peek`; nil discard guarded in shared TUI state
- Public key fingerprints are unique at the schema level

### Notes

- Poker rule/evaluator code is present but **not registered** and not playable in v0.1
