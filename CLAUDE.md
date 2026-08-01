# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test-short        # unit tests, no Docker
make test              # go test -race ./...
make test-integration  # -tags=integration, needs Docker (testcontainers)
make lint              # golangci-lint
make build             # -> bin/server
make ci                # fmt, fix, lint, test, build

go test -race -run TestName ./internal/game/poker/          # single test
go test -race -run 'TestX/subtest_name' ./internal/lobby/   # single subtest

make migrate-create    # prompts for name, writes internal/db/migrations/
make migrate-up        # needs $DB_DSN exported
```

Local multi-client testing: `./scripts/dev-session.sh` opens three tmux-attached SSH clients against a running server (`TC_PORT=6969` bypasses the nginx proxy).

## Architecture

SSH connection → wish middleware → Bubble Tea `Router` → view → `game.BoundEngine` → `game.Engine` → `Rules`.

### Game registration is a single point

`internal/catalog/catalog.go` `All` is the only place a game is declared, and each entry carries both the rules factory and the TUI view constructor. `cmd/server/main.go` builds the `game.Registry` from it; `internal/tui/app.go` registers routes from it. Rules and view therefore cannot drift. `catalog_test.go` fails on a missing field or duplicate slug.

Two identifiers, two consumers: `Module.Name` (display name) is the registry key and what `db.Game.Name` stores; `Slug` is what the TUI derives routes from (`router.GameRoute(slug)` → `"game_<slug>"`). `internal/game` deliberately knows nothing about routes.

### Layer boundaries

- `internal/db` — GORM models **and** the `UserRepository` / `MatchRepository` interfaces. It defines the contract.
- `internal/repository` — the GORM implementations. Everything else depends on the `db` interfaces, never on this package (except `internal/ssh` for its error sentinels).
- `internal/game` — pure rules/engine, no db, no TUI, no routes.
- `internal/tui` — presentation only; reaches state through `router.GlobalContext`.

### Engine and Rules contract

`Engine` holds `e.mu` and `state.mu` together for the whole of `Start`, `SubmitAction`, and `RemovePlayer`. `Rules` methods are called with both held, so a `Rules` implementation must never call back into `Engine` (deadlock) and may mutate `*State` freely.

Per-game state lives in `State.Extra` (`crazyeight.State`, `poker.State`). The turn cursor is settled by `applyNextTurnLocked`: `State.OverrideNextTurn` wins, else advance, else honor `State.CurrentTurn` — that is how poker picks the next actor from `AfterAction`. `ApplyAction` runs then `AfterAction`; an `AfterAction` error finishes the game, so anything checkable up front belongs in `ValidateAction`.

Mid-hand disconnects: implement the optional `game.PlayerLeaveHandler` (`OnPlayerLeave` before removal, `AfterPlayerRemoved` after seat indices shift).

### BoundEngine, not Engine, in views

`game.Bind(engine, playerID)` gives a session-scoped handle that only submits as that player and only returns that player's hand. TUI views use `BoundEngine` / `SyncBaseState`; `Engine.WithState` and `SubmitAction` are for the server side.

### Subscription lifecycle

`broadcaster.Broadcaster[T]` is latest-wins (drops the oldest on a full buffer) and hands back a *closed* channel when at capacity — engines size it `len(players)+8` for the ranked-finalize watcher and reconnect overlap.

Any view holding a subscription must implement `router.Closer`. The router closes the active view on navigation; `ssh.releaseSession` closes the whole model on disconnect. Skipping `Close()` parks a listener goroutine and burns a subscriber slot until the engine closes.

Lock order when both are involved is manager (`m.mu`) then lobby (`l.mu`) — see `Manager.Kick` / `RemoveLobby`.

### SSH server

Middleware in `wish.WithMiddleware` runs **last-first**, so `sessionLifecycle` is listed last to be outermost: charmbracelet/ssh runs handlers in a goroutine with no recover, so an escaped panic would kill the process. `releaseSession` must stay a direct `defer` for `recover()` to work. Auth accepts any public key; identity is the SHA256 fingerprint, first connection registers the username.

The backend listens on `:6969` behind nginx speaking PROXY protocol. Publishing that port lets clients spoof source IPs and defeat the per-IP rate limiter.

## Conventions

- Production schema changes are SQL migrations in `internal/db/migrations/` (up **and** down). `AutoMigrate` is test-only (`internal/testutil/db.go`).
- Integration tests are behind `//go:build integration`; `testutil.SetupTestDB` skips when Docker is absent.
- Table-driven tests with named subtests, `t.Parallel()` where safe. `pgregory.net/rapid` for property tests, `goleak` for goroutine leaks.
- Wrap errors with `%w`, lowercase messages.
- The `.golangci.yml` size gates (`funlen` 75/55, `cyclop` 21, `gocognit` 30, `nestif` 9, `lll` 140) sit just above the worst surviving function. Split the function rather than raising a threshold.
- Comments explain *why*, not *what*; the codebase is intentionally light on them.
- `rules/` is a vendored Go/GORM code-review skill set, not application code.
