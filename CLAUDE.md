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

SSH connection -> wish middleware -> Bubble Tea `Router` -> view -> `game.BoundEngine` -> `game.Engine` -> `Rules`.

### Game registration is a single point

`internal/catalog/catalog.go` `All` is the only place a game is declared, and each entry carries both the rules factory and the TUI view constructor. `cmd/server/main.go` builds the `game.Registry` from it; `internal/tui/app.go` registers routes from it. A game cannot be registered without a view (`catalog_test.go` fails on a missing field or duplicate slug); copying an entry and changing only the rules still compiles, so keep the pair in lockstep by hand.

Two identifiers, two consumers: `Module.Name` (display name) is the registry key and what `db.Game.Name` stores; `Slug` is what the TUI derives routes from (`router.GameRoute(slug)` -> `"game_<slug>"`). `internal/game` deliberately knows nothing about routes.

### Shared helpers, not per-game copies

`internal/deck` owns card mechanics every game needs: `RemoveOne`/`RemoveEach` (hand
removal that never aliases), and three distinct rank questions that must not be
swapped — `RankValue` (Ace high, 14, for poker and Hearts), `RunOrder` (Ace low 1..13,
courts distinct, for Gin Rummy runs) and `PipValue` (courts count 10, for deadwood).
Standard ranks are 1-based, so a zero `deck.Card` is detectably empty rather than the
ace of spades; Uno's extra ranks sit in their own block at 20+. `AllRanks` is what
makes `Rank`-keyed maps testable for exhaustiveness.

`game.AnyScoreAtLeast` is the shared match-target check for Hearts and Gin Rummy.

### Layer boundaries

- `internal/db` - GORM models **and** the `UserRepository` / `MatchRepository` interfaces. It defines the contract.
- `internal/repository` - the GORM implementations. Everything else depends on the `db` interfaces, never on this package (except `cmd/server`, which is the composition root, and `internal/ssh` for its error sentinels).
- `internal/game` - pure rules/engine, no db, no TUI, no routes. Seat identity is the scalars on `game.Player` (`UserID`, `Name`, `Ratings`), not a `*db.User`.
- `internal/tui` - presentation only; reaches state through `router.GlobalContext`.

### Engine and Rules contract

`Engine` holds `e.mu` and `state.mu` together for the whole of `Start`, `SubmitAction`, and `RemovePlayer`. `Rules` methods are called with both held, so a `Rules` implementation must never call back into `Engine` (deadlock) and may mutate `*State` freely.

Per-game state lives in `State.Extra` (`crazyeight.State`, `poker.State`, `uno.State`, `hearts.State`, `ginrummy.State`). The turn cursor is settled by `applyNextTurnLocked`: `State.OverrideNextTurn` wins, else advance, else honor `State.CurrentTurn` - that is how poker picks the next actor from `AfterAction`. `ApplyAction` runs then `AfterAction`; an `AfterAction` error finishes the game, so anything checkable up front belongs in `ValidateAction`.

Mid-hand disconnects: implement the optional `game.PlayerLeaveHandler` (`OnPlayerLeave` before removal, `AfterPlayerRemoved` after seat indices shift).

### Turn clock

`applyNextTurnLocked` also arms a per-turn timer (`DefaultTurnTimeout`, 30s). On expiry the engine plays the move from the optional `game.TurnTimeoutHandler` (`TimeoutAction`) and broadcasts `EventTurnTimedOut`; after `MaxMissedTurns` (3) consecutive expiries it re-checks under the engine lock and only then broadcasts `EventPlayerIdle` and removes the seat. A player's own action clears their count, so this only fires on someone who stopped playing.

Rules opt in: no `TurnTimeoutHandler` means no clock. Poker checks when free, folds when not, and deals between hands (an absent dealer would otherwise freeze the table); crazy eights and uno draw; hearts passes its three lowest cards, plays its first legal card, and deals the next hand; gin rummy draws, sheds its priciest deadwood and deals. `TimeoutAction` must return something `ValidateAction` accepts, or the turn re-arms and the seat is taken on the next expiry instead — gin rummy's `autoDiscard` skips the card the upcard rule forbids for exactly this reason.

`game.TurnDurationHandler` lets a rules set stretch a particular turn: hearts gives the pass phase 45s and the between-hands prompt a minute; poker and gin rummy stretch the between-hands deal the same way. Returning zero keeps the engine default; it cannot resurrect a clock `WithTurnTimeout` disabled.

`turnSeq` is what makes this safe: every cursor change invalidates timers already in flight, so a player who acted as their clock ran out is not charged a miss. `stopTurnTimerLocked` runs from `finishGameLocked`, the last-player-standing path, and `Close`.

The game view quits its bubbletea program on its own `EventPlayerIdle`, which is what ends the ssh session through the ordinary `releaseSession` path - the engine never reaches into the session layer.

### BoundEngine, not Engine, in views

`game.Bind(engine, playerID)` gives a session-scoped handle that only submits as that player and only returns that player's hand. TUI views use `BoundEngine` / `SyncBaseState`; `Engine.WithState` and `SubmitAction` are for the server side.

It is a façade, not a capability: `BoundEngine.Engine()` still reaches whole-table state, and poker uses it because rendering a table means rendering every seat. The value is that the default path is the safe one, so reaching past it is a visible detour and the redaction becomes the view's stated job (`buildSeats`).

### gameview.Session is the view baseline

Every game view embeds `gameview.Session` (`internal/tui/views/game/session.go`). It owns the parts that are the same in all five games: binding to the engine, subscribing (`NewSession`), reading the feed (`Listen`/`EventMsg`), losing a seat to the idle timer (`IdleRemoved`), the hand cursor (`MoveCursor` / `SelectDigit` / `SelectedCard`), leaving the table (`Leave`), and `Close` — which is what satisfies `router.Closer`. A new game implements its own rules rendering and nothing else.

Read per-game state through `Session.WithHiddenState` — named for what it hands back, unredacted table state the view has to filter itself — and seat order, display names and stock size through `BaseState` (`Seats`, `SeatOrder()`, `SeatNames()`, `DeckSize`) rather than reaching back through `Engine().WithState` — `PlayerSnapshot.Username` already falls back to the player ID.

Anything a view keeps after releasing the engine lock must be copied, not aliased (`maps.Clone`, `HandResult.Clone`).

### Subscription lifecycle

`broadcaster.Broadcaster[T]` is latest-wins (drops the oldest on a full buffer) and `Subscribe` returns `ErrAtCapacity` / `ErrClosed` rather than a pre-closed channel, so a caller cannot mistake "you will never receive anything" for "the stream ended". Engines size it `len(players)+8` for the ranked-finalize watcher and reconnect overlap. Views surface a failure in their own error line; the lobby logs it loudly because it means a match result will not be persisted.

Any view holding a subscription must implement `router.Closer`. The router closes the active view on navigation; `ssh.releaseSession` closes the whole model on disconnect. Skipping `Close()` parks a listener goroutine and burns a subscriber slot until the engine closes.

Lock order when both are involved is manager (`m.mu`) then lobby (`l.mu`) - see `Manager.Kick` / `RemoveLobby`.

### SSH server

Middleware in `wish.WithMiddleware` runs **last-first**, so `sessionLifecycle` is listed last to be outermost: charm.land/ssh runs handlers in a goroutine with no recover, so an escaped panic would kill the process. `releaseSession` must stay a direct `defer` for `recover()` to work. Auth accepts any public key; identity is the SHA256 fingerprint, first connection registers the username.

### Stats API

`internal/httpapi` serves a read-only JSON feed the website reads: `/v1/stats`
(players online, hands in play, tables open) and `/v1/leaderboard?limit=N`. It is
mounted by `cmd/server/main.go` on `API_PORT` (6970) and reached only through the
proxy's `/api/` location - never published to the host.

It is deliberately narrow: no writes, no auth, no per-user data, nothing the TUI
leaderboard does not already show any visitor. That is what makes it safe
unauthenticated. Live counts come from `ssh.SessionTracker.Count` and
`lobby.Manager.Stats`, so `SetupServer` accepts an optional `Tracker` for sharing.

`API_TRUST_PROXY` makes the per-network limiter read `X-Forwarded-For`. It **defaults
to false** and compose opts in explicitly: a directly exposed listener that trusts the
header can be evaded by forging it, so the unsafe direction has to be chosen. nginx
sets it from `$remote_addr`, not `$proxy_add_x_forwarded_for`, so a client cannot
prepend its own value.

Both limiters key on `ratelimit.NetKey`, which collapses IPv6 to its /64. Keying on the
full address is meaningless there: one customer is routinely handed 2^64 of them.

The backend listens on `:6969` behind nginx speaking PROXY protocol. Publishing that port lets clients spoof source IPs and defeat the per-IP rate limiter.

## Conventions

- Schema changes are SQL migrations in `internal/db/migrations/` (up **and** down). `testutil.SetupTestDB` applies those same files, so the tested schema cannot drift from the deployed one.
- Integration tests are behind `//go:build integration`; `testutil.SetupTestDB` skips when Docker is absent.
- Table-driven tests with named subtests, `t.Parallel()` where safe. `pgregory.net/rapid` for property tests, `goleak` for goroutine leaks.
- Wrap errors with `%w`, lowercase messages.
- The `.golangci.yml` size gates (`funlen` 75/55, `cyclop` 21, `gocognit` 30, `nestif` 9, `lll` 140) sit just above the worst surviving function. Split the function rather than raising a threshold.
- Comments explain *why*, not *what*; the codebase is intentionally light on them.
- `rules/` is a vendored Go/GORM code-review skill set, not application code.
