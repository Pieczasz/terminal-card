# Reading guide - how to learn this codebase

A curated tour for a new engineer (or an agent) who needs to understand **design
and data flow**, not just file names. Read with the code open. When a section
says “open X”, open that file and skim before moving on.

**Companions**

| Doc | Role |
|---|---|
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Contracts, topology, invariants |
| [`ONBOARDING.md`](ONBOARDING.md) | Product context, patterns, ops |
| [`CLAUDE.md`](CLAUDE.md) | Short agent brief (often the freshest operational truth) |
| This file | Ordered walk + “where is X?” + tooling |

**Suggested pace:** Days 1-2 = Parts A-C (spine). Day 3 = Part D (game engine).
Day 4 = Part E (one full game + its TUI). Day 5 = Part F (persist + ops). Keep
Part G (tooling) beside you the whole time.

---

## Part G first? Tooling that makes reading easier

You can read everything by hand. These tools cut the search tax.

### GitNexus (recommended for AI-assisted onboarding)

[GitNexus](https://github.com/abhigyanpatwari/GitNexus) indexes the repo into a
**local knowledge graph** (calls, imports, processes) and exposes it to Cursor /
Claude Code via MCP. Agents stop grepping blindly and ask “what calls
`finalizeFinishedGame`?” / “blast radius of changing `SessionTracker`?”.

```bash
# once per machine
npx gitnexus setup

# from this repo root - index + register (skip injecting CLAUDE.md/skills)
npx gitnexus analyze . --index-only --skip-agents-md --skip-skills

# useful CLI once indexed
npx gitnexus impact finalizeFinishedGame -d 5
npx gitnexus context DisconnectPlayer
npx gitnexus query -q "match end persistence" -g "EventGameEnded to MatchRepository"
```

[`ARCHITECTURE.md`](ARCHITECTURE.md) §10 was filled from these queries on
`main@843ff1b` (4.1k nodes / 19k edges). Re-run after the spine moves.

**What it is good for:** call chains, impact before a refactor, finding every
implementer of `Rules`, spotting that Submit↛Finalize is an async hop.
**What it is not:** product design narrative. Still read Parts A-C by hand.
**Caveat:** channel receives and `time.AfterFunc` do not become CALLS edges - if
`trace A B` returns `no_path`, check for a broadcaster/timer boundary before
assuming the path is dead.

### Built-in Go tools (no install drama)

```bash
# who imports lobby?
go list -f '{{.ImportPath}} {{.Imports}}' ./... | rg lobby

# reverse: packages that import this package
go list -f '{{.ImportPath}}' -deps ./cmd/server | sort -u

# call graph sketch (needs graphviz for pretty pictures)
go install golang.org/x/tools/cmd/callgraph@latest
callgraph -format digraph ./cmd/server | head

# jump to definition / references in your editor
# Cursor: Cmd-click / “Go to References”
```

### Editor + agent habits that work on this repo

1. Keep [`CLAUDE.md`](CLAUDE.md) in context - layer boundaries and turn-clock
   rules are there for a reason.
2. Prefer **one vertical slice** (SSH -> lobby -> engine -> finalize) over reading
   every game package first.
3. When stuck, search for the **type name** (`SessionAPI`, `finalizeRequest`,
   `disconnectGrace`) not the English phrase.
4. Tests are documentation: `internal/lobby/finalize_test.go`,
   `internal/ssh/lifecycle_test.go`, `internal/game/poker/streets_test.go`.

### Other options (optional)

| Tool | Use |
|---|---|
| [Sourcegraph](https://sourcegraph.com) / Cody | Cross-repo search if you host many services |
| [GoLand](https://www.jetbrains.com/go/) / VS Code Go | Best-in-class “Find Usages” for interfaces |
| `go doc ./internal/game` | Package overview from comments |
| Grafana dashboards in compose | Runtime confirmation after you understand the paths |
| Blog posts under `web/src/content/blog/` | Narrative essays (`one-process-holds-the-table`, broadcaster, frames) |

---

## Mental model (one page)

```
 SSH session (per channel) 
  wish middleware -> Bubble Tea Router -> View       
                                                 
                                                 
                lobby.SessionAPI / BoundEngine    
                                                  
  SessionTracker (userID -> generation)             

                       
                       
              lobby.Manager (process-wide)
                         
                          disconnectGrace (90s mid-game)
                 
              lobby.Lobby start-> game.Engine
                                       
                  requestFinalize Events
                                       
           Manager.finalize* watcher goroutine
                 
                 
           MatchRepository -> Postgres
```

Live play never hits the database. Only auth/profile/leaderboard reads and
match finalization write paths do.

---

## Part A - Composition root and trust boundary

### A1. Open `cmd/server/main.go`

Read in order: `main` -> `run` -> repository construction -> `lobby.NewManager` ->
`ssh.SetupServer` -> `serve` -> `drainServer`.

**Watch for:** defer order (OTel, DB close, `waitForFinalizers`). Accept-loop
failure and API failure both drain - a process that dies mid-match must not rate
Elo.

**Data flow:** env -> `config.Config` -> deps struct -> long-lived Manager + SSH
server.

### A2. Open `internal/config/config.go`

Env parsing, `PROXY_PROTOCOL`, production validation. Then peek at
`internal/config/nginx.conf` - PROXY protocol and why `:6969` stays private.

### A3. Open `compose.yaml` (skim)

Service graph: proxy -> backend, migrate -> db, alloy -> LGTM. Confirm backend
ports are not published.

**Checkpoint:** you can explain why publishing 6969 is a security bug.

---

## Part B - Identity and session lifecycle

### B1. `internal/ssh/auth.go`

`AuthenticateSession` -> fingerprint. `LoadOrRegisterUser` ->
`db.UserRepository`. Sentinels: `db.ErrUsernameTaken` /
`ErrInvalidUsername` / `ErrKeyAlreadyRegistered` in `internal/db/errors.go`.

### B2. `internal/ssh/server.go` - read these symbols first

| Symbol | Meaning |
|---|---|
| `sessionLifecycle` | Outermost middleware; span + recover + teardown |
| `sessionModel` | Auth -> Connect -> build TUI |
| `SessionTracker` | `active map[uint]uint64` generations |
| `Connect` / `Owns` / `Release` | Displace vs capacity |
| `releaseSession` | Only the owning generation leaves the seat |
| `reportingModel` | TUI panic -> metric + clean leave |

**Data flow (connect):**

```
TCP -> PROXY -> Wish -> PublicKeyAuth (rate limited)
  -> sessionLifecycle
  -> sessionModel
      LoadOrRegisterUser
      tracker.Connect -> gen
      tui.Model(...)
      st.gen = gen
```

**Data flow (disconnect / displace):**

```
channel close
  -> releaseSession
      if !Owns(user, gen): return // displaced zombie
      LobbyManager.DisconnectPlayer // grace if mid-game
      tracker.Release(user, gen)
      model.Close() // unsubscribe views
```

### B3. Tests that teach

- `internal/ssh/lifecycle_test.go` - displace, seat-before-slot ordering
- `internal/ssh/server_test.go` - `TestServer_SecondSessionDisplaces`

**Checkpoint:** you can explain why `ErrAlreadyConnected` is gone.

---

## Part C - TUI routing and lobby surface

### C1. `internal/tui/app.go` + `internal/tui/router/router.go`

`ModelDependencies.LobbyManager` is `lobby.SessionAPI`. Router owns
`GlobalContext`, swaps views, closes `Closer`s, idle watchdog.

Open `internal/lobby/session.go` - the exact method list the UI may call.

### C2. Catalog wiring

`internal/catalog/catalog.go` - each game = rules factory + view factory.
`app.go` registers `game_<slug>` routes from the same slice `main.go` used for
the registry.

### C3. Lobby views (read order)

1. `internal/tui/views/lobby/create.go` - `WithCardGame(name)`
2. `internal/tui/views/lobby/join.go` - `BrowseLobbies` + 2s refresh + memo
3. `internal/tui/views/lobby/lobby.go` - ready, kick, settings, start -> navigate
   to game route with engine payload

### C4. Resume path

In `app.go`, before home: `ResumePlayer` -> if lobby in game, jump to game view;
if waiting, jump to lobby view. This is how a wifi blip returns you to the table.

**Checkpoint:** TUI never imports `internal/repository` for matches.

---

## Part D - Lobby manager, grace, finalize

Read package files in this order:

### D1. `internal/lobby/manager.go`

Maps: `lobbies`, `playerLobby`. Methods: `New`, `JoinLobbyByCode`, `Kick`,
`LeaveLobby`, `DisconnectPlayer`, `ResumePlayer`, `BeginShutdown`,
`registerFinalizer` / `WaitForFinalizers`.

### D2. `internal/lobby/disconnect.go`

Tiny state machine: `pending` timers -> `expiring` claim -> `LeaveLobby`.
`tryCancel` returns blocked when expire already owns the seat.

### D3. `internal/lobby/lobby.go` (skim by symbol)

| Symbol | Role |
|---|---|
| `options.cardGame string` | Domain game id |
| `ToggleReady` / `startGameLocked` | Match birth |
| `watchGameLocked` / `handleBroadcasterEvents` | Idle leave + finalize |
| `requestFinalize` | Snapshot -> Manager |
| `releaseFinishedGame` | Back to Waiting |

### D4. `internal/lobby/finalize.go`

`finalizeFinishedGame` -> `persistFinishedMatch` -> `recordFinishedMatch`.
Rating gate:

```text
rated = isRanked && !shuttingDown && reason != EndReasonRulesError
```

### D5. `internal/lobby/browse.go`

`BrowseLobbies`, cache dirty flag, Elo distance sort, `GameNames`.

### D6. Tests

`finalize_test.go` (rules-error unrated, grace TOCTOU, takeover),
`manager_test.go`, `browse_test.go`.

**Data flow (match end):**

```
Engine finishGameLocked -> EventGameEnded
  -> Lobby.handleBroadcasterEvents
  -> requestFinalize(finalizeRequest{code, gameName, ranked, startedAt})
  -> Manager.finalizeFinishedGame
      registerFinalizer
      StandingsWithPlaces
      recordFinishedMatch -> MatchRepository
  -> releaseFinishedGame (lobby Waiting again)
```

**Checkpoint:** you know which package writes Elo, and when it refuses to.

---

## Part E - Game engine and one full game

### E1. Core interfaces - read top to bottom

1. `internal/game/rules.go` - `Rules` + optional handlers
2. `internal/game/state.go` - phase, Extra, OverrideNextTurn
3. `internal/game/action.go` - `Action`, `Event`, `EndReason`
4. `internal/game/engine.go` - `SubmitAction`, `RemovePlayer`, `Frame`,
   `finishGameLocked`, `applyNextTurnLocked`
5. `internal/game/turnclock.go` - timeout auto-play + idle kick
6. `internal/game/bound.go` - per-player façade
7. `internal/game/shed.go` - shared stock reshuffle helpers
8. `internal/deck/card.go` - `RankValue` / `RunOrder` / `PipValue` (do not swap)

### E2. View baseline

1. `internal/tui/views/game/session.go` - `Sync`, `HandleFrame`, `Leave`, `Close`
2. `internal/tui/views/game/state.go` - `BaseState`
3. `internal/tui/views/game/frame.go` - shared Update loop pieces
4. `internal/tui/views/game/layout.go` / `zones.go` - rendering bands

### E3. Pick one game and go vertical (recommended: Crazy Eights first)

| Layer | Files |
|---|---|
| Rules | `internal/game/crazyeight/rules.go`, `state.go` |
| Catalog | entry in `catalog.go` |
| TUI | `internal/tui/views/game/crazyeight/{model,update,view}.go` |

Trace one action: keypress -> `Update` -> `Bound.Submit` -> `Validate/Apply` ->
event -> `Listen` -> `Sync` -> `View`.

### E4. Then poker (hardest rules)

`internal/game/poker/{rules,streets,state,evaluator}.go` +
`internal/tui/views/game/poker/model.go` (single `Sync`/`Frame` hold for the
whole table - showdown needs every seat).

Read `TestLeave_HeadsUpAllInDoesNotForfeit` - leave × engine last-seat logic.

### E5. Remaining games (same shape)

| Game | Rules pkg | View pkg |
|---|---|---|
| Uno | `internal/game/uno` | `…/views/game/uno` |
| Hearts | `internal/game/hearts` | `…/views/game/hearts` |
| Gin Rummy | `internal/game/ginrummy` | `…/views/game/ginrummy` |

**Checkpoint:** you can add a sixth game by copying the catalog pair without
touching the engine.

---

## Part F - Persistence, Elo, HTTP, observability

### F1. Contract then implementation

1. `internal/db/repository.go` - interfaces
2. `internal/db/users.go`, `games.go`, `matches.go` - models
3. `internal/db/migrations/*.sql` - schema truth
4. `internal/repository/user.go` - register + profile + leaderboard cache
5. `internal/repository/match.go` - **read `FinalizeRankedMatch` carefully**

Inside ranked finalize, note:

- `lockPairing` / `pairingAdvisoryKey`
- soft-delete revive in `seedRankingRows`
- provisional `MatchesPlayed`
- `samePairingCountLast24h` damping

### F2. Elo math

`internal/elo/elo.go` - pure functions; repository applies them under row locks.

### F3. Read-only API

`internal/httpapi/httpapi.go` - `/v1/stats`, `/v1/leaderboard`. Fed by session
count + manager stats. Unauthenticated on purpose; no per-user data.

### F4. Observability

`internal/observability/metrics.go` + `otel.go`. Compose ships Alloy ->
Loki/Tempo/Prometheus -> Grafana. Use dashboards to confirm finalize outcomes and
session counts after you understand the code paths.

---

## Package map (current non-test layout)

```
cmd/server/main.go composition root, drain
cmd/loadtest/ SSH concurrency harness

internal/
  catalog/ single game registration list
  config/ env + nginx/alloy/prom/grafana assets
  db/ models, interfaces, errors.go, migrations
  repository/ GORM UserRepository + MatchRepository
  ssh/ Wish server, auth, SessionTracker
  lobby/ Manager, Lobby, browse, grace, finalize, SessionAPI
  game/ Engine, Rules, BoundEngine, turn clock, shed
    crazyeight|uno|hearts|ginrummy|poker/
  deck/ cards + shared hand helpers
  elo/ rating math
  broadcaster/ latest-wins fan-out
  tui/
    app.go, router/ session root
    components/ cards, fan, picker, table
    styles/ theme + pad
    views/home|lobby|profile|leaderboard|game/...
  httpapi/ public stats JSON
  observability/ OTel + metrics
  ratelimit/ sliding window + NetKey (/64)
  testutil/ shared test DB (applies migrations)
```

---

## “Where is X?” index

| Question | Start here |
|---|---|
| How does a key become a user? | `ssh/auth.go`, `repository/user.go` |
| Second SSH session for same account? | `SessionTracker.Connect` displace |
| Mid-game wifi drop? | `DisconnectPlayer`, `disconnect.go`, `ResumePlayer` |
| Who writes Elo? | `lobby/finalize.go` -> `repository/match.go` |
| Why didn’t Elo move? | shutdown / `EndReasonRulesError` / pairing damp / provisional |
| Soft-deleted ranking broke finalize? | `seedRankingRows` revive |
| How is a game registered? | `catalog/catalog.go` |
| Turn auto-play / idle kick? | `game/turnclock.go` |
| TUI reading engine state? | `Session.Sync` -> `BoundEngine.Frame` |
| Poker double-read bug class? | `views/game/poker/model.go` single Frame |
| Browse list sorting? | `lobby/browse.go` |
| Lock order? | Manager then Lobby - `ARCHITECTURE.md` §4.6 |
| Rate limit keying on IPv6? | `ratelimit/netkey.go` |
| Add a migration? | `make migrate-create`, files under `db/migrations/` |

---

## Suggested “prove you understand it” exercises

Do these against a local server (`make build`, Postgres, `PROXY_PROTOCOL=false`
for bare ssh, or `./scripts/dev-session.sh`).

1. Register two keys, create a casual Crazy Eights table, play to the end - find
   the match row and zero Elo deltas.
2. Start a ranked hand, kill one SSH client mid-hand, reconnect within 90s -
   confirm resume, not forfeit.
3. Connect a second session for the same key while the first is half-open -
   confirm displace + resume, not “already connected”.
4. Force a rules-error end in a test (`finalize_test.go`) - confirm casual
   record path for a ranked lobby.
5. Sketch on paper the lock order for `Kick` and for `BrowseLobbies`.

---

## How to keep this guide honest

When you change a contract (SessionTracker, finalize ownership, grace, Frame
signature, catalog shape), update in this order:

1. [`CLAUDE.md`](CLAUDE.md) (agents hit it first)
2. [`ARCHITECTURE.md`](ARCHITECTURE.md) §3-§8
3. This file’s Part that covers the path
4. [`ONBOARDING.md`](ONBOARDING.md) only if the day-one story changed

Do not leave commit SHAs as the source of truth for long; replace the “current
as of” line when the spine moves.
