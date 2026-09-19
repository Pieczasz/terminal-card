# Architecture

Current as of `main` @ `843ff1b`. Cross-checked against a local GitNexus index
(4,142 nodes / 18,950 edges / 352 flows; `npx gitnexus analyze . --index-only`).
Call chains in §10 were taken from `gitnexus impact` / `context` on that index.

For a file-by-file reading tour, start with [`READING_GUIDE.md`](READING_GUIDE.md).
For day-one setup and product context, see [`ONBOARDING.md`](ONBOARDING.md).
Agents: [`CLAUDE.md`](CLAUDE.md).

---

## 1. What this is

An SSH server that serves a terminal UI. You `ssh` in and get a Bubble Tea app:
home, lobby browser, and card games against other people on the same process.
Identity is the SSH public-key fingerprint. Results persist to Postgres with Elo.

Five games: **Crazy Eights**, **Uno**, **Hearts**, **Gin Rummy**, **Texas Hold'em**.

**One process holds the table.** Lobbies, engines, and hands live only in RAM.
Postgres stores what must outlive the process: users, keys, games, rankings,
match history. There is no Redis, no message broker, no shared cache.

A read-only JSON API (`internal/httpapi`) feeds the marketing site. No game HTTP
API, no WebSockets.

---

## 2. Topology

```
        ssh client (port 22)
                 
                 
   
    nginx (stream proxy) internal/config/nginx.conf
     limit_conn 8 / client IP PROXY protocol on
   
                    PROXY header (real client IP)
                   
   
    backend :6969 (Go) cmd/server
     Wish SSH + Bubble Tea        
     lobby.Manager (in memory)    
     game.Engine per match        
   
                          
                          
      
    Postgres 16 Alloy OTLP -> Loki/Tempo/Prom 
    users, keys -> Grafana       
    games, ranks     
    matches       
   
```

**Never publish `:6969`.** The backend trusts PROXY protocol for client IP. A
direct client could forge that header and defeat per-IP rate limits. Compose
keeps 6969 Docker-internal; only nginx may reach it. Local bare-ssh needs
`PROXY_PROTOCOL=false` (see `scripts/dev-session.sh`).

### Defence in depth

| Layer | Limit | Where |
|---|---|---|
| nginx | 8 conns / client IP | `nginx.conf` |
| listener | `MAX_CONNECTIONS` (default 1000) | `netutil.LimitListener` |
| auth | 5 handshakes / s / IP | `ratelimit`, `RATE_LIMIT_*` |
| channels | 2 sessions / SSH connection | `maxSessionsPerConnection` |
| account | one live slot / user; **second Connect displaces** | `ssh.SessionTracker` |
| lobby join | 10 / s / player | `Manager.joinLimiter` |
| TUI idle | quit after 5 min idle (not mid-game) | `router.Router` |
| SSH idle | Wish idle timeout (30 min) | `wish.WithIdleTimeout` |

---

## 3. End-to-end spine

### 3.1 Boot - `cmd/server/main.go`

Order matters so deferred teardown runs correctly (LIFO):

1. `config.Load()` -> validated `*config.Config`
2. `observability.SetupOTel` (defer shutdown)
3. `db.Connect` (defer close)
4. Repos -> `lobby.NewManager(ctx, matchRepo)`
5. `defer waitForFinalizers(lobbyManager)` **before** DB close in LIFO terms
   (registered after DB close defer -> drains match writes first)
6. `game.Registry` from `catalog.All`
7. `ssh.SetupServer` -> `serve(...)`

`serve` stacks `tcp -> LimitListener -> proxyproto.Listener`, runs `Serve` in a
goroutine, blocks on signal / accept error / API error. Every exit path calls
`drainServer` (`BeginShutdown` + graceful SSH stop) so ranked matches interrupted
by deploy are recorded **without** Elo.

### 3.2 Connection - `internal/ssh`

Wish middleware runs **last-first**. `sessionLifecycle` is listed last so it is
outermost (span + recover + teardown).

1. Auth: any public key accepted; identity is `SHA256:<fingerprint>`
2. `LoadOrRegisterUser` - first connection claims the SSH username
3. `tracker.Connect(userID)` -> generation. A second session for the same account
   **displaces** the first (half-open TCP otherwise blocks reconnect for the whole
   mid-game grace window). Capacity -> `ErrServerFull`
4. Build TUI; stash `sessionState{user, model, gen}`
5. On teardown: `releaseSession` - if `tracker.Owns(user, gen)`, then
   `DisconnectPlayer` + `Release`; a displaced gen touches neither seat nor slot

Auth sentinels (`db.ErrUsernameTaken`, …) live in `internal/db/errors.go`. SSH
depends on the `db` contract package, not `internal/repository`.

### 3.3 TUI - `internal/tui`

`tui.Model` builds `router.Router` with `GlobalContext`:

- `User`, `UserRepository`
- `LobbyManager` as **`lobby.SessionAPI`** (not the full `*Manager`)
- `GameRegistry`, `SessionCtx`

There is **no `MatchRepository` on the TUI**. Persistence is owned by
`lobby.Manager` after a game ends.

Initial route: `ResumePlayer` -> mid-game reconnect lands back at the table;
otherwise home / waiting lobby.

Game routes come from `catalog.All` via `router.GameRoute(slug)` -> `"game_<slug>"`.
`internal/game` knows nothing about routes.

### 3.4 Lobby - `internal/lobby`

- `WithCardGame(name string)` - domain id is the game **name** string (registry
  key / `db.Game.Name`), not a `*db.Game` pointer
- `BrowseLobbies(player, BrowseFilter)` - public waiting tables, Elo-sorted,
  cached ~2s, hard-capped
- Ready -> `startGameLocked` -> `registry.Create(name)` -> `game.NewEngine` ->
  subscribe a finalize watcher -> broadcast `EventGameStarted` with the engine

Mid-game disconnect: `DisconnectPlayer` arms **`DisconnectGrace` (90s)** via
`disconnectGrace` (`disconnect.go`). `ResumePlayer` cancels a pending leave, or
returns the seat on takeover with no pending leave. Waiting-lobby seats and
shutdown still leave immediately. `expireLeave` moves pending -> expiring under
the manager lock so a reconnect cannot resume a seat about to vanish.

### 3.5 Play - `internal/game`

```
key -> view.Update -> BoundEngine.Submit(action)
                         
                         
                 Engine.SubmitAction (e.mu held)
                   ValidateAction -> ApplyAction -> AfterAction
                   CheckWinCondition / applyNextTurnLocked
                   broadcast Event*
```

Views sync with `Session.Sync(fn)` -> `BoundEngine.Frame(fn func(*State))`: one lock
hold for snapshot, own hand, turn clock, and live `*State` (Extra is unredacted -
copy what you keep).

### 3.6 Finish - `Manager.finalizeFinishedGame`

Lobby watcher sees `EventGameEnded` -> `requestFinalize` (snapshot under lobby
RLock) -> **`Manager.finalizeFinishedGame`** (`finalize.go`):

1. `registerFinalizer` (refuse if shutdown already stopped new finalizers)
2. `StandingsWithPlaces` (ties share place when rules implement `StandingScorer`)
3. Rate only when ranked **and** not shutting down **and** reason ≠ `EndReasonRulesError`
4. `FinalizeRankedMatch` or `RecordCasualMatch`

Then `releaseFinishedGame` reopens the lobby so settings work without another ready.

---

## 4. Contracts (the ones that bite)

### 4.1 Rules <-> Engine

`Rules` in `internal/game/rules.go`. Engine holds **one** mutex (`Engine.mu`) for
`Start` / `SubmitAction` / `RemovePlayer` / `Frame`. `State` has no lock.

- Rules must **never** call back into `Engine` (deadlock)
- Rules may mutate `*State` freely under the engine lock
- `ValidateAction` rejects cleanly; `ApplyAction` / `AfterAction` errors finish the
  game as `EndReasonRulesError` (may be half-applied) -> **unrated** persistence
- Optional: `PlayerLeaveHandler`, `TurnTimeoutHandler`, `TurnDurationHandler`,
  `StandingScorer`

Per-game state: `State.Extra` (`*crazyeight.State`, `*poker.State`, …).

### 4.2 Turn cursor - `applyNextTurnLocked`

1. `OverrideNextTurn` if set (then clear)
2. else advance if requested
3. else honour `CurrentTurn`

Also arms the turn clock (`turnSeq` fences stale timers). No
`TurnTimeoutHandler` -> no clock. After `MaxMissedTurns` consecutive expiries ->
`EventPlayerIdle` + seat removal. Only **accepted** actions clear the miss count.

`OverrideNextTurn` also keeps a last-seat-standing hand open (poker HU all-in
leave still contests the pot).

### 4.3 BoundEngine

`Bind(engine, playerID)` - default safe path: submit as self, hand is yours,
`Frame` is one coherent read. Not a capability boundary: `Engine()` still reaches
whole-table state (poker showdown needs every seat).

### 4.4 Session / view baseline

Every game view embeds `gameview.Session`: subscribe, `HandleFrame`, cursor,
leave, `Close` (`router.Closer`). Skipping `Close` parks a listener and burns a
subscriber slot.

### 4.5 Subscriptions

`broadcaster.Broadcaster[T]` is latest-wins. `Subscribe` returns
`ErrAtCapacity` / `ErrClosed` rather than a pre-closed channel. Engines size
`len(players)+8` for finalize watcher + reconnect overlap.

### 4.6 Lock order

Manager (`m.mu`) **then** lobby (`l.mu`). Never invert. Browse cache
invalidation uses an atomic dirty flag so lobby state changes do not take
manager lock while holding lobby lock.

---

## 5. Persistence and anti-farm Elo

Interfaces in `internal/db`; GORM in `internal/repository`.

**Ranked finalize** (`FinalizeRankedMatch`), one transaction:

1. `pg_advisory_xact_lock` on the sorted participant set (`lockPairing`) so the
   same pair cannot underrun 24h damping by racing Poker and Hearts
2. Revive soft-deleted ranking rows, then seed missing ones
3. `SELECT … FOR UPDATE` rankings (ordered by user id)
4. Provisional accounts / pairing damping / Elo update
5. Write match + participants + places + deltas

**Casual:** `RecordCasualMatch` - history only, no Elo.

Migrations live in `internal/db/migrations/` (init + constraints + provisional
columns). `testutil.SetupTestDB` applies the same files.

---

## 6. Catalog - single registration point

`internal/catalog/catalog.go` `All` is the only place a game is declared. Each
entry carries rules factory **and** TUI view constructor. `cmd/server` builds the
registry; `internal/tui/app.go` registers routes. Missing field / duplicate slug
fails `catalog_test.go`.

| Field | Consumer |
|---|---|
| `Module.Name` | registry key, lobby option, `db.Game.Name` |
| `Slug` | TUI route `game_<slug>` |

---

## 7. Package responsibilities

| Package | Owns | Must not |
|---|---|---|
| `cmd/server` | composition root, drain | game rules |
| `internal/ssh` | transport, auth, session gen | match writes |
| `internal/tui` | presentation | `MatchRepository` |
| `internal/lobby` | tables, grace, finalize orchestration | card rules |
| `internal/game` | engine + rules | db, tui, routes |
| `internal/db` | models + repo **interfaces** + auth sentinels | GORM queries |
| `internal/repository` | GORM implementations | SSH / TUI |
| `internal/catalog` | game list | runtime state |
| `internal/httpapi` | read-only stats/leaderboard | writes / auth |
| `internal/deck` | shared card helpers | game-specific rules |

Lobby file split:

| File | Role |
|---|---|
| `manager.go` | maps, join/new/kick, shutdown |
| `lobby.go` | roster, ready, start, watcher -> `requestFinalize` |
| `finalize.go` | persist finished matches |
| `disconnect.go` | mid-game grace maps |
| `session.go` | `SessionAPI` for TUI |
| `browse.go` | public list + `GameNames` |

---

## 8. Invariants that bite

1. Never publish `:6969` with PROXY trust on.
2. `sessionLifecycle` outermost; recover is a **direct** defer.
3. Displaced session must not `LeaveLobby` / free the tracker slot.
4. Mid-game drop -> `DisconnectPlayer`, not `LeaveLobby`.
5. Finalize on Manager; RulesError and shutdown -> history without Elo.
6. Soft-deleted rankings must be revived before seed or finalize aborts.
7. Same pairing across games shares the advisory lock + 24h damp count.
8. Views that subscribe implement `Closer`; router/`releaseSession` call it.
9. Anything kept after `Frame`/`Sync` must be copied, not aliased.
10. Lock order: manager -> lobby.
11. Catalog entry = rules + view; do not register one without the other.
12. Accept-loop / API failure still runs `drainServer`.

---

## 9. Doc map

| Doc | Use |
|---|---|
| [`READING_GUIDE.md`](READING_GUIDE.md) | Ordered file tour, data-flow maps, tooling (GitNexus, …) |
| [`ONBOARDING.md`](ONBOARDING.md) | Product context, patterns, run/test |
| [`CLAUDE.md`](CLAUDE.md) | Short ops brief for agents |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to add a game / PR norms |

---

## 10. Graph-verified call chains (GitNexus)

Indexed with GitNexus 1.6.11 against this tree. These are **static CALLS edges**,
not runtime traces. Async hops (channel receive of `EventGameEnded`, timer
callbacks) do **not** appear as a single path - that is expected and important.

### 10.1 Play a card - `BoundEngine.Submit` impact

```
BoundEngine.Submit
  -> Engine.SubmitAction
    -> Engine.submitActionLocked
      -> Rules.ValidateAction / ApplyAction / AfterAction / CheckWinCondition
      -> Engine.applyNextTurnLocked
      -> Engine.finishGameLocked (on win or rules error)
```

Module blast from Submit reaches Game + every rules package (Poker, Uno, Hearts,
Gin Rummy, Crazy Eights) via the `Rules` interface - OCP in practice.

There is **no static path** from `Submit` to `finalizeFinishedGame`: the engine
broadcasts `EventGameEnded`, and a different goroutine (`handleBroadcasterEvents`)
picks it up. Searching only the call graph will miss that seam.

### 10.2 Match persistence - `finalizeFinishedGame` impact

```
Lobby.requestFinalize (only static caller)
  -> Manager.finalizeFinishedGame
      -> registerFinalizer / shutdownCtx
      -> observability.GameFinished / MatchFinalize
      -> persistFinishedMatch
          -> Engine.StandingsWithPlaces
              -> standingsLocked / placesLocked
          -> isShuttingDown (rating gate)
          -> recordFinishedMatch
              -> MatchRepository.FinalizeRankedMatch (iface + gorm impl)
              -> MatchRepository.RecordCasualMatch
```

Who arms the watcher that eventually calls `requestFinalize`:

```
Lobby.startGameLocked
  -> watchGameLocked
    -> handleBroadcasterEvents
      -> requestFinalize (on EventGameEnded / finished fallback)
```

### 10.3 Session teardown - `releaseSession` impact

```
releaseSession
  -> SessionTracker.Owns / Release
  -> Manager.DisconnectPlayer
      -> disconnectGrace.arm (mid-game)
      -> OR LeaveLobby / expireLeave (waiting / grace end)
          -> beginExpire / clear
          -> Lobby.notifyEngineAndBroadcast
              -> Engine.RemovePlayer
          -> Manager.RemoveLobby (empty table)
```

Generation fencing sits on the SSH side (`Owns`/`Release`); grace fencing sits on
the lobby side (`pending` -> `expiring`). Both must agree or reconnect races.

### 10.4 Re-query locally

```bash
npx gitnexus analyze . --index-only --skip-agents-md --skip-skills
npx gitnexus impact --uid 'Method:internal/lobby/finalize.go:Manager.finalizeFinishedGame#3' -d 5
npx gitnexus context finalizeFinishedGame
npx gitnexus query -q "disconnect grace" -g "who holds the seat after SSH drop"
```

Re-run `analyze` after large merges; the index under `.gitnexus/` is local (not
committed). Absent edges across channels/timers mean “dynamic hop”, not “dead code”.
