# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Leaderboard game filter: TUI `g`/arrows cycle All + catalog games; `GET /v1/leaderboard?game=Uno`
- Uno (2–10 players): colour/number/symbol matching, Skip/Reverse/Draw Two/Wild/Wild Draw Four, deadlock detection
- Hearts (exactly 4): pass cycle, follow-suit and first-trick rules, hearts-breaking, shoot the moon, multi-hand match to 100
- Gin Rummy (exactly 2): meld search, automated layoffs, knock/gin/undercut scoring, stock wall
- `gameview.Session` — the subscribe/listen/cursor/leave/close plumbing every game view embeds
- `deck.RemoveOne`/`RemoveEach`, `deck.RankValue`/`RunOrder`/`PipValue`, `deck.AllRanks`, `game.AnyScoreAtLeast`
- No-Limit Texas Hold'em: full street/betting rules, all-in and side pots, showdown via `EvaluateHand`
- Poker Bubble Tea table UI (up to nine seats) and registry wiring (`poker` module)
- One ranked hand per match; rematch by Ready in a lobby

### Fixed

- Ranked match finalize failed on drifted local DBs missing `matches.ranked`; schema is a single `000001_init` again (pre-prod squash) and compose recreate applies it
- Gin Rummy: the card taken from the discard pile can no longer be laid straight back, and `MaxHandTurns` bounds a hand that would otherwise never reach the wall — discard draws never touch the stock, so two players could trade one card forever
- Gin Rummy: `TimeoutAction` no longer proposes a discard the rules would reject, which would have cost an absent player their seat
- Gin Rummy: the between-hands turn cursor no longer aliases a pointer into `State.Extra`
- Gin Rummy: layoffs prefer run attachments over sets; meld mask order is sorted so scoring is deterministic
- Hand removal helpers no longer panic on an empty hand (`make([]Card, 0, len(hand)-1)`)
- Gin Rummy `HandResult` is cloned out of engine state instead of shared by pointer with the view
- Poker: sub-minimum all-ins no longer reopen betting for players who already acted
- Poker showdown mini-cards use the shared rank table (Two–Nine were off-by-one after 1-based ranks)
- Elo no longer mints/destroys rating at the floor/ceiling; tied neighbours score 0.5/0.5
- Idle kick re-verifies under the engine lock so a player who acted in the timeout window keeps their seat
- Rate limiter evicts when full instead of locking out every new network
- SSH PTY width/height are clamped; session panic recovery covers cleanup, not only the program
- Private lobby browse cache is invalidated on leader settings changes
- `Config.DSN` emits a URL so empty/space-bearing passwords cannot drop the database name
- Graceful SSH shutdown always `Close`s after the drain window

### Changed

- Standard `deck.Rank` values are 1-based, so a zero `Card` is no longer the ace of spades; Uno's extra ranks moved to their own block clear of the playing-card ordering
- `game.Player` holds seat scalars (`UserID`, `Name`, `Ratings`); `internal/player` removed so `internal/game` no longer depends on `internal/db`
- All five game views embed `gameview.Session`; per-view `listenForEvents`/`gameMsg`/`unsubscribe`/`handleEscape` copies removed
- Seat order, display names and stock size come from `BaseState` (`CurrentPlayerID` for turn identity) rather than a second trip through `Engine().WithState`
- `BoundEngine` exposes `Subscribe`/`Unsubscribe` instead of the raw broadcaster; views read Extra via `WithHiddenState`

### Security

- Lobby join/create are atomic; mid-game joins are rejected; host actions require a leader actor
- Manager.Kick cleans `playerLobby`; join-by-code rate-limited; lobby codes are eight chars
- Deck shuffle and first-player selection use `crypto/rand`; `Engine.Start` fails closed if shuffle fails
- Elo ratings clamped to `[100, 4000]` before `uint32` storage
- Registration errors are sanitized for SSH clients; Wish uses structured session logging
- Hands cloned for TUI; game logs no longer dump player hands
- TUI submits actions via `BoundEngine` (session-bound player ID)
- Profile/leaderboard never render raw DB errors to SSH clients
- Broadcaster enforces max subscribers; rate limiter caps tracked keys
- Empty lobbies set `Closed` before broadcast to close join races
- Host key permissions re-checked on a load; `.dockerignore` excludes `.env`
- Production rejects remote `DB_SSLMODE=disable` unless `ALLOW_INSECURE_DB=true`
- Go toolchain bumped to 1.26.5 (crypto/tls ECH fix)
- Container image runs as non-root; nginx stream uses `limit_conn` per client IP
- New lobbies default to casual; ranked is opt-in via Mode toggle (limits Elo farming under open registration)
- Crazy Eights mid-game reshuffle fails closed if `crypto/rand` errors

### Changed

- New lobbies default to **casual**; create/lobby UI exposes Casual/Ranked Mode toggle
- Module path is now `github.com/Pieczasz/terminal-card`
- DB/integration tests require `-tags=integration`
- App root context is canceled on SIGTERM before SSH/DB shutdown

### Fixed

- Crazy Eights mid-game reshuffle fails closed (restores discard, forced pass) if `crypto/rand` errors; leave-path shuffle errors are logged
- Self-hosting docs: PROXY/`6969` must stay Docker-internal; expanded Hetzner hardening checklist
- Ranked match ELO + history now commit in one transaction (`FinalizeRankedMatch`)
- Lobby getters lock shared state; creating a lobby requires a card game
- User+key registration is transactional; duplicate fingerprint rejected cleanly
- Broadcaster subscribers unsubscribed on leave/disconnect; finalize no longer closes the engine early
- Rate limiter filters per-IP on the hot path (periodic full sweep)
- DB pool size decoupled from SSH max connections (`DB_MAX_OPEN_CONNS`)
- OTLP TLS controllable via `OTEL_EXPORTER_OTLP_INSECURE`; service version from env/build
- Post-condition failures finish the game instead of leaving a torn playable state
- Ready/start re-checks readiness under the lobby lock
- SQL DB closed on graceful shutdown; MigrateDSN URL-encodes credentials
- Join-by-code handles post-join lookup failure without nil lobby panic
- Empty `UpdateRankings` returns an empty map instead of nil

## [0.1.0] - 2026-07-12

### Added

- SSH multiplayer server with Bubble Tea TUI
- Crazy Eights as the first playable game
- PostgreSQL persistence for users, matches, and ELO
- `game.Module` registry for plug-in game registration and TUI routing
- Docker Compose deploy path with migrated job and persistent SSH host keys
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
