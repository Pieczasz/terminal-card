# terminal-card - architecture and code guide

Personal reading guide. Not committed, not for the repo. Written against `main` at
`e3b246e` (merge of PR #6).

Read order if you're new to a part of it:
1. §1 and §2 for the shape of the thing.
2. §3 - the end-to-end flow. Everything else is detail hanging off this.
3. §4 - the five contracts. If you only remember one section, remember this one.
4. §5–§10 per subsystem, as needed.
5. §12 before you change anything (invariants that bite), §13 for recipes.

---

## 1. What this is

An SSH server that serves a terminal UI. You `ssh terminal-card.example` and get a
full-screen Bubble Tea app: a home screen, a lobby browser, and card games played
against other people connected to the same process. Identity is your SSH key.
Results persist to Postgres with Elo ratings.

Two games ship today: **Crazy Eights** and **Texas Hold'em poker**.

Single process, single node. ~17k lines of Go across ~70 source files and ~50 test
files. No HTTP API, no frontend build, no message broker.

The whole thing is built around one idea: **the terminal is the client, the SSH
session is the transport, and the process holds all live state in memory.** The
database is only for things that must outlive the process (accounts, Elo, match
history). Lobbies, engines, and hands exist only in RAM and die with the process.

---

## 2. Topology

```
        ssh client (a human, port 22)
                 │
                 ▼
   ┌───────────────────────────────┐
   │ nginx (stream proxy)          │  internal/config/nginx.conf
   │  | listen :22                 │  | limit_conn 8 per client IP
   │  | proxy_protocol on          │  | proxy_timeout 1h
   └───────────────┬───────────────┘
                   │ PROXY protocol, real client IP preserved
                   ▼
   ┌───────────────────────────────┐
   │ backend :6969 (Go, scratch)   │  cmd/server
   │  | wish SSH server            │
   │  | Bubble Tea program/session │
   │  | lobby.Manager (in memory)  │
   │  | game.Engine per match      │
   └───────┬───────────────┬───────┘
           │               │
           ▼               ▼
   ┌───────────────┐   ┌──────────────────────────────────┐
   │ Postgres 16   │   │ Alloy :4317 (OTLP)               │
   │  users        │   │   ├─ logs    → Loki              │
   │  public_keys  │   │   ├─ traces  → Tempo             │
   │  games        │   │   └─ metrics → Prometheus        │
   │  rankings     │   │        all three → Grafana :3000 │
   │  matches      │   └──────────────────────────────────┘
   │  match_parts  │
   └───────────────┘
```

`compose.yaml` wires all of it. **Port 6969 is deliberately not published.** The
backend trusts the PROXY protocol header for the client IP; if clients could reach
6969 directly they could forge that header and defeat the per-IP rate limiter and
poison the logs. Only the `proxy` service may reach the backend.

Ports 3000/3100/3200/9090/4317/4318 are bound to `127.0.0.1` only.

### Defence in depth on connections

| Layer | Limit | Where |
|---|---|---|
| nginx | 8 concurrent conns per client IP | `nginx.conf` `limit_conn ssh_addr 8` |
| listener | 1000 total concurrent conns | `netutil.LimitListener`, `MAX_CONNECTIONS` |
| auth | 5 handshakes per second per IP | `ratelimit.SlidingWindowLimiter`, `RATE_LIMIT_*` |
| account | 1 live session per user | `ssh.SessionTracker` |
| lobby join | 10 attempts/sec per player | `Manager.joinLimiter` |
| idle | quit after 5 min without input, unless in a game | `router.Router.Update` |

---

## 3. The end-to-end flow

This is the spine. Follow it once and the packages make sense.

### 3.1 Boot - `cmd/server/main.go`

`main` → `run()`, which does everything in an order chosen so the **deferred
teardown runs correctly in reverse**:

1. `config.Load()` - env → `*config.Config`, validated.
2. `observability.SetupOTel(ctx, cfg)` - logger/tracer/meter providers, runtime
   metrics, app metrics. Returns a `shutdown` func; deferred with a 5s budget.
3. `defer` the "server exited with error" log. Registered **after** the OTel
   shutdown so LIFO runs it **first**, while the logger provider is still alive.
   Reverse that order and the final error log vanishes into a torn-down exporter.
4. `slog.SetDefault` with `observability.NewFanoutHandler(jsonHandler, otelHandler)`
   - every record goes to stderr *and* the OTLP exporter.
5. `db.Connect(cfg)` → `*gorm.DB`; `defer sqlDB.Close()`.
6. Repositories, then `lobby.NewManager(ctx, matchRepo)`.
7. `defer waitForFinalizers(lobbyManager)` - registered **after** the DB close
   defer, so LIFO drains in-flight match writes *before* the handle they write
   through is closed. 15s soft deadline, then waits indefinitely.
8. Build `game.Registry` from `catalog.All`.
9. `ssh.SetupServer(deps)`, then `serve(...)`.

`serve` builds the listener stack `tcp → LimitListener → proxyproto.Listener`,
starts `server.Serve` in a goroutine, and blocks on SIGINT/SIGTERM. On signal:
30s graceful `Shutdown`, then unwind, which flushes telemetry and drains writes.

`sshServer` is a two-method interface (`Serve`, `Shutdown`) purely as a **test
seam** - the accept-loop failure branches are unreachable through a real
`*charmssh.Server`, whose `Serve` blocks on a live listener.

### 3.2 A connection arrives - `internal/ssh`

`wish.NewServer` is configured with a middleware slice. **wish runs the slice
last-first**, so it reads in reverse execution order:

```go
wish.WithMiddleware(
    bm.Middleware(sessionModel(deps, tracker)),  // 4th (innermost): the TUI program
    activeterm.Middleware(),                     // 3rd: reject non-interactive
    logging.StructuredMiddleware(),              // 2nd: connect/disconnect logs
    sessionLifecycle(deps, tracker),             // 1st (outermost): span + recover
)
```

`sessionLifecycle` **must** be outermost: `charm.land/ssh` runs each handler in a
goroutine with no recover, so a panic anywhere inside would kill the whole process
and every other player's game with it. It:
- opens the `ssh.session` span and stashes its context under `ctxKeyTraceCtx`,
- `defer releaseSession(...)` - written as a **direct defer on purpose**. `recover()`
  only stops a panic when called by the deferred function itself; wrapping it in a
  helper would silently let panics escape. Don't refactor this.

Auth: `wish.WithPublicKeyAuth(rateLimitAuth(limiter, alwaysTrue))`. **Any public
key is accepted.** Identity is `SHA256:<fingerprint>` of the offered key.

`sessionModel` then:
1. `AuthenticateSession(s)` → fingerprint (`auth.go`).
2. `LoadOrRegisterUser(ctx, repo, s.User(), fingerprint)` - look up by fingerprint;
   if unknown, register the SSH username as a new account bound to that key. **First
   connection claims the username permanently.**
3. `tracker.Connect(user.ID)` - refuses a second concurrent session per account.
4. Builds the TUI root model and stashes it under `ctxKeyModel` so teardown can
   close it.

`releaseSession` on every disconnect: recover → `model.Close()` (releases broadcaster
subscriptions) → `tracker.Disconnect` → `LobbyManager.LeaveLobby`.

### 3.3 The TUI - `internal/tui`

`tui.Model(deps)` builds a `router.Router` and registers every route, then
`Goto(RouteHome)`. The Router **is** the `tea.Model` handed to Bubble Tea.

Router responsibilities:
- own the `GlobalContext` (user, repos, lobby manager, registry, session ctx, W/H),
- swap the active view on `ChangeViewMsg`,
- call `Close()` on the outgoing view if it implements `router.Closer`,
- track `lastActivity`; a 10s `tick` quits sessions idle >5 min **unless** the
  active route starts with `game_` (nobody gets kicked out of a live hand),
- force `AltScreen` on every rendered view.

Game routes are registered from `catalog.All` via `router.GameRoute(slug)` →
`"game_<slug>"`. `internal/game` deliberately knows nothing about routes.

### 3.4 Creating and joining a lobby - `internal/lobby`

`Manager` holds `lobbies map[code]*Lobby` and `playerLobby map[playerID]*Lobby`.
Codes are 8 chars from `[A-Z0-9]`, crypto/rand, up to 10 attempts for uniqueness.

- `Manager.New(leader, opts...)` - validates game + max players, refuses if the
  player is already in a lobby, creates the `Lobby` with a `broadcaster.New[Event](maxPlayers)`.
- `Manager.JoinLobbyByCode(code, p)` - rate limited per player, then `lobby.addGuest`.
- `Manager.PublicLobbies(p)` - served from a **2-second cache**, then sorted by
  |lobby average Elo − player's Elo for that game| so the closest match is first.
  Every sort key is snapshotted before sorting because `GameName()`/`averageElo()`
  each take the lobby lock and a comparator must not re-lock.

Settings (private/ranked/max players/game) are leader-only and only while `Waiting`,
funnelled through `withLeaderSettings`, which broadcasts `SETTINGS_UPDATED`.

`ToggleReady` is the trigger: when every player is ready, `startGameLocked` runs.

### 3.5 Starting a match - `Lobby.startGameLocked`

```go
rules   := registry.Create(cardGame.Name)     // fresh Rules per match
engine  := game.NewEngine(rules, players, rules.InitialDeck())
engine.Start()
go l.handleBroadcasterEvents(engine.Broadcaster().Subscribe(), engine)  // result watcher
l.state = InGame; l.activeEngine = engine
l.broadcastUnlocked(Event{Type: EventGameStarted, Payload: engine})
```

The lobby view receives `GAME_STARTED`, pulls the `*game.Engine` out of the payload
and emits `ChangeViewMsg{ViewName: GameRoute(slug), Context: engine}`. The router
builds the game view via the catalog's `View` constructor, which calls
`game.Bind(engine, playerID)` and subscribes to the engine broadcaster.

### 3.6 Playing - `internal/game`

```
keypress → view.Update → BoundEngine.Submit(action)
                              │
                              ▼
                      Engine.SubmitAction(playerID, action)
                        e.mu.Lock + state.mu.Lock  (both, whole call)
                        ├─ phase must be Playing
                        ├─ playerID must be on turn
                        ├─ Rules.ValidateAction   → error returns to caller only
                        ├─ Rules.ApplyAction      (mutates state)
                        ├─ Rules.AfterAction      (rules advance their machine)
                        │     └─ on error: finishGameLocked → EventGameEnded
                        ├─ broadcast EventActionApplied
                        ├─ Rules.CheckWinCondition
                        │     └─ true: finishGameLocked → EventGameEnded, return
                        ├─ applyNextTurnLocked(advance = true)
                        └─ broadcast EventTurnAdvanced
```

Every subscriber's `listenForEvents` command wakes with a `gameMsg`, the view calls
`syncState()`, re-reads the engine under the state lock, and re-renders.

### 3.7 Finishing - persistence

`Lobby.handleBroadcasterEvents` is parked on the engine broadcaster for the life of
the match. On `EventGameEnded` it calls `finalizeFinishedGame(engine)`:

1. `engine.Standings()` → ordered `[]*player.Player`, 1st place first.
2. Map to DB user IDs; bail out (logged) if any player has no `DatabaseUser`.
3. Snapshot `isRanked` + `gameName` under the lobby read lock.
4. `manager.registerFinalizer()` - refuses if shutdown has begun; otherwise
   `finalizing.Add(1)`, released by `defer`.
5. 15s context, then `recordFinishedMatch`:
   - **ranked** → `MatchRepository.FinalizeRankedMatch` - one transaction that
     creates/looks up the game, updates Elo with `SELECT … FOR UPDATE` on the
     rankings, and writes the match + participants with their deltas.
   - **casual** → `GetOrCreateGame` + `RecordMatch(..., nil, false)` - history only,
     zero deltas, `matches.ranked = false`.

The profile view reads this back with `UserMatchHistory` and renders
`Poker: 1st place (Elo change: +16)` or `Poker: 2nd place (casual game)`.

---

## 4. The five contracts

Almost every bug I've seen in this codebase was a violation of one of these.

### 4.1 Rules ↔ Engine

`game.Rules` (`internal/game/rules.go`) is the whole game-logic surface:

```go
MinPlayers/MaxPlayers/InitialDeck/InitialDealCount
OnGameStart(state) error
ValidateAction(state, action) error
ApplyAction(state, action)
AfterAction(state, action) error
CheckWinCondition(state) bool
Standings(state) []*player.Player
```

Optional: `game.PlayerLeaveHandler` - `OnPlayerLeave` (before removal),
`AfterPlayerRemoved` (after seat indices shift).

**The engine holds `e.mu` and `state.mu` for the entire duration of `Start`,
`SubmitAction` and `RemovePlayer`, and calls Rules with both held.** Therefore:

- a `Rules` implementation must **never** call back into the `Engine` - instant
  deadlock;
- it may mutate `*State` freely, including `state.Deck`, `state.Players[i].Cards`,
  `state.CurrentTurn` and `state.OverrideNextTurn`;
- `ValidateAction` is the only place that can reject a move cleanly. An error from
  `AfterAction` **ends the game** (`finishGameLocked`), so anything checkable up
  front belongs in `ValidateAction`.

Per-game state lives in `State.Extra any` - `*crazyeight.State` or `*poker.State`.
Always type-assert with the `, ok` form; every call site does.

### 4.2 Turn resolution - `applyNextTurnLocked`

Precedence, in order:

1. `state.OverrideNextTurn != nil` → use it, then clear it.
2. `advance == true` → `turnManager.Next()`.
3. otherwise → honour `state.CurrentTurn`.

Then always `clampCurrent()` so a stale index can't panic, and write the result back
to `state.CurrentTurn` so the two never disagree.

This is how poker picks a non-adjacent next actor from `AfterAction`. **If you write
a rule that needs a specific next seat, you must set `OverrideNextTurn` - setting
`CurrentTurn` alone loses to `advance`.**

### 4.3 BoundEngine, not Engine, in views

`game.Bind(engine, playerID)` returns a session-scoped handle:
- `Submit` only ever submits as that player,
- `Hand()` only returns that player's cards (defensive copy),
- `Snapshot()` returns public table state and hand *sizes*, never other hands.

Views use `BoundEngine` + `gameview.SyncBaseState`. `Engine.WithState` and
`Engine.SubmitAction` are for the server side. A view reaching for the raw engine is
how you leak another player's hand.

`Engine.SnapshotFor(viewer)` currently ignores `viewer` - the parameter is reserved
for per-viewer redaction. Poker does its own redaction in `buildSeats`.

### 4.4 Subscription lifecycle

`broadcaster.Broadcaster[T]`:
- per-subscriber buffered channel, **256** deep,
- **latest-wins**: on a full buffer it drops the oldest and enqueues the newest, so a
  slow client sees current state rather than a backlog,
- at capacity it hands back a **closed channel** rather than blocking - engines size
  themselves `len(players)+8` to leave room for the ranked-finalize watcher and brief
  reconnect overlap. Size it too tightly and a real player's view freezes on a closed
  channel.

**Any view holding a subscription must implement `router.Closer`.** The router closes
the active view on navigation; `ssh.releaseSession` closes the whole model on
disconnect. Skipping `Close()` parks a listener goroutine and burns a subscriber slot
until the engine itself closes. Both game views have a `goleak_test.go` guarding this.

### 4.4b The theme is per session, not global

`styles.Theme` is a value on `router.GlobalContext`, resolved from
`tea.BackgroundColorMsg` (`IsDark()`) and defaulting to dark when the terminal
never answers the OSC 11 query. It is **not** a package-level palette, because two
players connected to the same process can have opposite terminal backgrounds - a
shared palette leaves one of them reading white on white.

It propagates exactly like `Width`/`Height`: `Router.Update` updates its own copy
(used to build views created later) and `views.HandleCommonMsg` updates the copy
held by the view currently on screen, so a mid-session theme switch takes effect
without navigating away.

Consequences for anything that draws:
- **No raw `lg.Color` outside `styles/theme.go`.** `TestNoRawColoursOutsideTheme`
  walks `internal/tui` and fails on one. Add a token instead.
- Free render helpers take `styles.Theme` as their first parameter
  (`components.RenderCard`, `gameview.RenderHand`, `renderChipStack`, …); methods
  read `m.global.Theme`.
- Text tokens are held to WCAG AA 4.5:1 and non-text to 3:1 against eight
  reference backgrounds (`theme_test.go`), so adding a token means proving it is
  legible in both modes.

### 4.5 Lock order

**Manager (`m.mu`) before lobby (`l.mu`), never the reverse.** `Manager.Kick` and
`Manager.RemoveLobby` are the two places that hold both; read them before writing a
third. `Lobby.finalizeFinishedGame` takes only `l.mu` (and `finalizerMu`, a leaf lock
that acquires nothing while held).

---

## 5. Package and file guide

### `cmd/server`
| File | What |
|---|---|
| `main.go` | `run()` composition root + `serve()` accept loop. Defer ordering matters - see §3.1. |
| `Dockerfile` | Multi-stage → `scratch`. Non-root uid 65532, ships `/data/ssh` pre-chowned so a fresh named volume is writable by keygen. |
| `main_test.go` | Drives `serve` through the `sshServer` seam. |

### `internal/catalog` - game registration
| File | What |
|---|---|
| `catalog.go` | `All []Entry` - **the only place a game is declared.** Each entry carries name, slug, rules factory *and* the TUI view constructor, so rules and view cannot drift. |
| `catalog_test.go` | Fails on a missing field or duplicate slug. |

Two identifiers with two consumers: `Module.Name` is the registry key and what
`db.Game.Name` stores; `Slug` is what the TUI derives routes from.

### `internal/config`
| File | What |
|---|---|
| `config.go` | Env → `Config`. `intEnvs` accumulates integer parse errors so one check covers all. `getEnv` treats `""` as absent (a blank compose var must not defeat a default). `Validate()` enforces production rules: DB password required, `sslmode=disable` only for internal hosts or with `ALLOW_INSECURE_DB=true`. |
| `nginx.conf`, `alloy/`, `prometheus/`, `tempo/`, `grafana/` | Infra configs, bind-mounted by compose. Living under `internal/` is unusual but keeps deploy config beside the code that needs it. |

### `internal/db` - models **and** interfaces
| File | What |
|---|---|
| `gorm.go` | `Connect` - pool: 10 idle, `DB_MAX_OPEN_CONNS` open (default 25), 1h lifetime. GORM logger at Info, Warn in production. |
| `users.go` | `User`, `PublicKey`, `Ranking` + `ValidateUsername` (≤16 chars, `[A-Za-z0-9_]`), enforced again by a DB check constraint. |
| `games.go` | `Game{Name uniqueIndex}`. |
| `matches.go` | `Match{GameID, Ranked}`, `MatchParticipant{MatchID, UserID, Placement, EloDelta}`. |
| `repository.go` | `UserRepository` and `MatchRepository` **interfaces**. This package defines the contract; everything depends on these, never on `internal/repository`. |
| `migrations/000001_init.{up,down}.sql` | The whole schema, including `matches.ranked`. |

### `internal/repository` - the GORM implementations
| File | What |
|---|---|
| `user.go` | Fingerprint lookup, transactional registration (checks username + fingerprint, maps unique violations to `ErrUsernameTaken`/`ErrKeyAlreadyRegistered` so a lost race reads correctly), `BestPlayers` with a **5-minute cache** over a top-100 query, `UserProfile`, `UserMatchHistory` (preloads Match→Game→Participants→User). |
| `match.go` | `FinalizeRankedMatch` = one transaction: `getOrCreateGame` (ON CONFLICT DO NOTHING + read-back) → `updateRankingsTx` (`SELECT … FOR UPDATE` on rankings so concurrent finalizes can't lose an Elo update) → `recordMatchTx`. `RecordMatch(..., ranked bool)` is the casual path. |

Only `internal/ssh` imports this package, and only for its error sentinels.

### `internal/game` - engine core, no DB, no TUI, no routes
| File | What |
|---|---|
| `rules.go` | The `Rules` and `PlayerLeaveHandler` interfaces. |
| `state.go` | `State`: players, left players, turn cursor, override, phase, winner, deck, discard, rules, `Extra`. `Phase`: Waiting→Dealing→Playing→Finished. |
| `engine.go` | `Start`, `SubmitAction`, `RemovePlayer`, `Standings`, `SnapshotFor`, `WithState`, `finishGameLocked`, `applyNextTurnLocked`, `Close`. |
| `turns.go` | `TurnManager` - forward-only cursor with `RemovePlayer` reindexing and `clampCurrent`. |
| `bound.go` | `BoundEngine` - the per-session capability. |
| `registry.go` | `Module` + `Registry`; `RegisterModule` **panics** on a malformed module (programmer error at boot, not runtime). |
| `action.go` | `Action` interface, `Event`/`EventType`, `PlayerSnapshot`, `StateSnapshot`. |

`standingsLocked` = rules standings, then any `LeftPlayers` **the rules did not place
themselves** (most recent leave first). Poker places its own leavers; Crazy Eights
doesn't, so the engine appends them.

`finishGameLocked(fallback)` is the single exit: sets `Finished`, picks the winner
from standings (falling back to the given player, then to seat 0), broadcasts
`EventGameEnded`. Used by the win path, the `AfterAction`-error path and
`RemovePlayer`, so a game can never end silently.

### `internal/game/crazyeight`
| File | What |
|---|---|
| `state.go` | `CurrentSuit`, `Passes`. |
| `rules.go` | 2–6 players, 7 cards each, one card face up to start. Play matches suit or rank; an eight is wild and **must** carry a chosen suit in the same action (`ActionPickSuit` alone is rejected - one action, one state change). Drawing is always legal: with an empty stock it reshuffles the discard (keeping the top card), and if that fails it **fails closed** by incrementing `Passes` rather than dealing from an unshuffled pile. `Passes >= len(players)` ends a deadlocked hand, scored by fewest cards. `OnPlayerLeave` returns the leaver's cards to the stock and reshuffles, so the deck stays whole. |

### `internal/game/poker`
| File | What |
|---|---|
| `state.go` | Seat markers, pot/bet/blind amounts, phase, per-player maps (chips, street bets, total contributed, folded, all-in, acted), `Pots`, `Winners`, `ReachedShowdown`, and the match fields `HandNumber`/`HandsTotal`/`MatchComplete`/`BustedAtHand`. |
| `rules.go` | Match lifecycle. `HandsPerMatch = 10`, stack 1000, blinds 25/50. `InitialDealCount() == 0` - poker owns its own deal so there is exactly one dealing path, `beginHand`. Actions: Fold, Check, Call, RaiseTo, AllIn, **NextHand**. |
| `streets.go` | Betting-round completion, next-to-act, street progression, board run-out, showdown, side pots, `rankPlayers`. |
| `evaluator.go` | 5–7 card hand evaluation. |

**Match model.** `CheckWinCondition` returns `MatchComplete`, not `HandComplete`.
A hand ending only pauses the table:

```
beginHand → betting rounds → finishHand
   ▲                              │
   │                              ├─ hands exhausted or ≤1 funded seat → MatchComplete
   └── ActionNextHand ────────────┘  else park turn on the next dealer
```

- `beginHand(state, extra, dealer)` - reset per-hand state, fresh shuffled deck, deal
  2 to every funded seat, mark busted seats folded and card-less so the cursor skips
  them, move the button, post blinds, pick first actor.
- **Funded seats are counted before the blinds are posted.** Count after and a table
  where the blinds bust two short stacks looks heads-up, handing the button first
  preflop action instead of UTG.
- `finishHand` stamps `BustedAtHand` for newly broke players, then either ends the
  match or parks the turn on the next funded seat, who deals with `ActionNextHand`.
- `ReachedShowdown` is set **only** by `runShowdown`. An uncontested pot is won
  face-down; with hands left to play, revealing those cards is a live information
  leak. `buildSeats` keys its reveal on this flag, not on `HandComplete`.
- `rankPlayers`: seated players first, leavers last (leaving forfeits), each group
  sorted by chips desc → bust-out hand desc (lasting longer places higher) → active
  before folded → hand score → ID. Everyone who busts is level on chips, so without
  the bust-out key their placement - and their Elo - would come down to
  `strings.Compare` on the player ID.

**Side pots** (`buildSidePots`): distinct non-zero contribution levels ascending, each
closing a pot layer; a layer with no eligible player carries forward as dead money;
odd chips are distributed deterministically after sorting the winner IDs.

**Evaluator.** Cards → `rankedCard{rank 2..14, suit}` sorted descending, then classified
into a `HandValue{Rank, Kickers[5]}` packed into one comparable int by `Score()`
(`rank<<20 | k0<<16 | … | k4`). Two subtleties worth knowing:
- `bestFlush` walks suits in a **fixed order**, not map order - ranging the map scored
  identical input differently between runs when two suits both reached five.
- `kickers` pads with zeros to the requested count, so `k[0]`/`k[1]` are always in
  range even for degenerate input. (CodeRabbit flagged an out-of-range panic here; it
  can't happen, and 437k fuzz executions agree.)
- `rankValue` maps `deck.Joker` onto 14, i.e. an ace. Unreachable today -
  `StandardDeck()` stops at King.

### `internal/lobby`
| File | What |
|---|---|
| `manager.go` | Code generation, lobby/player maps, join rate limit, public-lobby cache + Elo-distance sort, `Kick`, `RemoveLobby`, `shutdownCtx`, `registerFinalizer`/`WaitForFinalizers`. |
| `lobby.go` | Roster, ready map, settings, per-player subscription bookkeeping, `startGameLocked`, `handleBroadcasterEvents`, `finalizeFinishedGame`, `recordFinishedMatch`. |

`playerSubs map[playerID][]<-chan Event` exists so a disconnect can unsubscribe that
player's channels without hunting through the broadcaster.

`RemoveLobby` nils out the engine and broadcaster under the lobby lock, then closes
them **outside** it.

### `internal/tui`
| File | What |
|---|---|
| `app.go` | Route table. Lobby create/join routes first check `FindLobbyByPlayer` and redirect into the existing lobby, so you can't end up with two. |
| `router/router.go` | `Router`, `GlobalContext`, `ChangeViewMsg`, `Closer`, idle-quit tick, AltScreen. |
| `views/common.go` | `SessionPlayer`, global shortcut table (`n`/`f`/`p`/`t`), `NavigateOn` (adds esc/q = home), `HandleCommonMsg` (window size, ctrl+c), `RenderCenteredLayout`. |
| `views/home/home.go` | Static screen; ASCII title via go-figure. |
| `views/lobby/create.go` | Game/visibility/ranked/max-players form; clamps max players to the selected game's `Min/MaxPlayers`. |
| `views/lobby/join.go` | Browse public lobbies or type an 8-char code. |
| `views/lobby/lobby.go` | Roster, ready marks, leader settings, kick, leave confirmation, and the `GAME_STARTED` → game-route hand-off. Implements `Closer`. |
| `views/leaderboard/leaderboard.go` | Top players from the cached `BestPlayers`. |
| `views/profile/profile.go` | Rankings + recent matches; `resultLabel` prints the Elo delta for ranked rows and `casual game` for casual ones. |
| `views/game/layout.go` | Shared card-table rendering: hand with selection lift, opponent card backs per table edge, turn status, waiting/finished screen. |
| `views/game/state.go` | `BaseState` + `SyncBaseState` - the redacted per-view snapshot every game view starts from. |
| `views/game/crazyeight/{model,update,view}.go` | Hand cursor, suit picker (2×2, cell width derived from the widest label so `♦ Diamonds` can't wrap), spring animation. |
| `views/game/poker/{model,update,view,chips}.go` | Seat snapshots + redaction, action bar, raise prompt with chip denominations (100/50/25/10 on keys 1–4), chip stacks drawn per seat and under the pot, hand-over/match-over screen. |
| `components/card.go` | One card face. |
| `styles/theme.go` | **The entire colour vocabulary.** `Theme` + `NewTheme(isDark)` built on `lipgloss.LightDark`, carrying semantic tokens (Text/TextMuted/TextDim, Accent/Heading/AccentAlt, Error/Success/Warning, Selection, Border/BorderMuted, CardFace/CardBack, SuitRed/SuitDark, TurnFg/TurnBg, Chips[4], Placements[3]) plus prebuilt styles. The only file allowed to name a colour. |
| `styles/common.go` | Box sizing (`BoxWidth`/`InnerWidth`/`AvailableContentHeight` - clamped so an ultra-wide terminal doesn't stretch the layout), `MinWidth`/`MinHeight` + `TooSmall`, `PadTruncate`, `RenderFigureASCII` (tries slant→small→mini, falls back to plain text), and the layout/footer renderers as `Theme` methods. |
| `animation/animation.go` | 60 FPS `Tick` + harmonica spring defaults. |

### `internal/observability`
| File | What |
|---|---|
| `otel.go` | `SetupOTel` - resource, propagators, logger/tracer/meter providers over OTLP gRPC, runtime instrumentation, app metrics. Every early-return path tears down already-started providers via the deferred `cleanup` (the named return is nil on exactly those paths, which is why the closure calls `cleanup` explicitly). |
| `metrics.go` | Three atomics: `SSHSessionsActive`, `GamesStartedTotal`, `RateLimitRejectsTotal`. |
| `loghandler.go` | `fanoutHandler` - writes each record to every sink, `errors.Join`s failures so one broken sink never hides the others. |

Metrics are **observable instruments** read from atomics on each collection cycle, so
the hot paths only do an atomic add.

### Supporting packages
| Package | What |
|---|---|
| `deck` | `Card{Rank,Suit}`, `Pile` (crypto/rand shuffle, Draw/DrawNCards/Add/Peek/Size), `StandardDeck()` = 52 cards, no jokers. `DrawNCards` rejects a negative count - a bad `InitialDealCount` used to panic the server. |
| `player` | `Player{ID, DatabaseUser, Cards}`; `Equal` prefers DB ID, falls back to string ID; `Username()` falls back to ID. |
| `elo` | Simple Multiplayer Elo: each player scored against **immediate neighbours only** (win vs. the one below, loss to the one above), K=32, clamped to [100, 4000], NaN logged and reset to default. |
| `ratelimit` | Sliding-window limiter, per-key timestamp log, `maxKeys` 10k with periodic eviction (every 64 ops) to bound memory under abuse. |
| `broadcaster` | §4.4. |
| `testutil` | `SetupTestDB` - testcontainers Postgres + `AutoMigrate`. `//go:build integration`; skips on `-short` or when Docker is absent. |
| `systemtest` | Black-box tests over the real manager/registry/engine, plus DB-backed persistence tests. |

---

## 6. Data model

```
users ──< public_keys        (fingerprint uniqueIndex → identity)
  │
  └──< rankings >── games    (PK user_id+game_id, elo CHECK 0..4000 DEFAULT 1500)
  │
  └──< match_participants >── matches ──> games
         placement, elo_delta            ranked bool
```

Every model uses `gorm.Model` (soft deletes via `deleted_at`).

**Production schema changes are SQL migrations** in `internal/db/migrations/`, up
**and** down. `AutoMigrate` is test-only (`internal/testutil/db.go`). The `migrate`
compose service runs migrations to completion before the backend starts.

Rating flow: standings order → `elo.Calculate` → deltas per user → `rankings.elo`
updated and `match_participants.elo_delta` recorded, all in one transaction.

---

## 7. Observability

- **Logs.** `slog` JSON to stderr *and* OTLP → Alloy → Loki. `logging.StructuredMiddleware`
  logs connect/disconnect; the rest is explicit `slog` calls.
- **Traces.** One `ssh.session` span per connection, its context stashed under
  `ctxKeyTraceCtx` and threaded into repository calls, which open `db.*` spans
  (`otel.Tracer("terminal-card/repository")`). So a slow profile load is attributable
  to a session and a user. → Tempo.
- **Metrics.** Go runtime instrumentation plus the three app counters. → Prometheus.
  Alloy also scrapes host metrics via `prometheus.exporter.unix`.
- **Grafana** on `127.0.0.1:3000`, anonymous admin, provisioned datasources and
  dashboards from `internal/config/grafana/`.

Everything funnels through Alloy on 4317/4318, so swapping a backend is a config
change, not a code change.

---

## 8. Testing

Three tiers, and the tooling is genuinely the strongest part of this repo.

| Tier | Command | What |
|---|---|---|
| Unit | `make test-short` | No Docker. DB tests skip themselves. |
| Race | `make test` | `go test -race ./...` |
| Integration | `make test-integration` | `-tags=integration`, testcontainers Postgres. |

CI (`.github/workflows/test.yml`) runs all three as separate jobs plus golangci-lint
v2.12.2. `make ci` = fmt, fix, lint, test, build.

**Conventions.** Table-driven with named subtests, `t.Parallel()` where safe, errors
wrapped with `%w` and lowercase messages, `paralleltest`/`tparallel`/`thelper`/
`testifylint`/`usetesting` enforced by the linter.

**Techniques in use:**
- **goleak** - `TestMain` in `broadcaster`, `lobby`, `systemtest`, and both game view
  packages. This is what stops a missing `Close()` from silently parking goroutines.
- **rapid** property tests - e.g. `TestChipsAreConservedAcrossRandomHands`: chips only
  move between stacks and the pool, so the total is invariant across any legal
  sequence of actions.
- **Fuzzing** - `FuzzEvaluateHand`, `FuzzClassifyHand`, `FuzzJoinLobbyByCode`,
  `FuzzValidateUsername`, `FuzzToUint32`, `FuzzPile_DrawNCards`. Seed corpora live in
  `testdata/fuzz/`; `go test` runs the seeds, `-fuzz` explores.
- **Black-box system tests** - `internal/systemtest` drives the real manager,
  registry and engine through a whole lobby→game→finalize journey, with a recording
  match repository that **signals** each write on a channel so tests wait for the
  write rather than polling or sleeping.
- **Test seams** - `sshServer` in `cmd/server`, `db.*Repository` interfaces
  everywhere else.

**Benchmarks:** `BenchmarkPlayFullMatch` (poker, 2/6/9 seats - a whole match must stay
well inside one 16ms frame), `BenchmarkEvaluateHand`, `BenchmarkSyncState`,
`BenchmarkBroadcast`, `BenchmarkCalculate`, `BenchmarkAllow`,
`BenchmarkFanoutHandler_Handle`.

**Lint gates** (`.golangci.yml`) are set *just above* the worst surviving function:
`funlen` 75/55, `cyclop` 21, `gocognit` 30, `nestif` 9, `lll` 140. The rule is: split
the function, don't raise the threshold. Test files are excluded from the size and
`wrapcheck` rules.

---

## 9. Local development

```bash
docker compose up -d          # full stack incl. observability
./scripts/dev-session.sh      # three tmux-attached SSH clients (one/two/three)
TC_PORT=6969 ./scripts/dev-session.sh   # bare `go run`, bypassing nginx
TC_LAYOUT=panes ./scripts/dev-session.sh
TC_DRY_RUN=1 ./scripts/dev-session.sh   # print commands only
```

The script needs three SSH keys (`~/.ssh/id_localhost_1`, `id_ed25519`,
`id_ed25519_second_user`) and uses `IdentitiesOnly=yes` so a loaded agent doesn't log
all three players in as the same user, plus `UserKnownHostsFile=/dev/null` because the
host key is regenerated whenever the volume is wiped.

Single test / subtest:
```bash
go test -race -run TestName ./internal/game/poker/
go test -race -run 'TestX/subtest_name' ./internal/lobby/
```

---

## 10. Conventions

- Comments explain **why**, not what. The codebase is intentionally light on them; a
  comment usually marks a trap.
- Errors wrapped with `%w`, messages lowercase, sentinels in the package that owns the
  concept.
- `rules/` (if present in your tree) is a vendored Go/GORM code-review skill set, not
  application code.
- Dependencies stay minimal: charm stack, GORM+pgx, OTel, testify, rapid, goleak,
  testcontainers, proxyproto, keygen, godotenv, harmonica, go-figure.

---

## 11. Design decisions and why

| Decision | Why | What it costs |
|---|---|---|
| Single process, in-memory state | No broker, no cache, no coordination. Everything is a method call. | No horizontal scaling; a restart drops every live game. `broadcaster.go` notes Watermill+Redis as the upgrade path. |
| Catalog as the single registration point | Rules and view are declared together, so they cannot drift. One test enforces it. | Adding a game touches one file - but that file imports both layers. |
| Engine holds both locks across Rules calls | Rules authors never think about concurrency and can mutate state freely. | Rules must never call back into the engine. Long rules work blocks the table. |
| `Extra any` for per-game state | The engine stays game-agnostic. | Type assertions at every access; no compile-time guarantee. |
| BoundEngine capability object | Redaction is structural, not a convention a view can forget. | One more indirection in every view. |
| Latest-wins broadcaster | A slow terminal sees current state, not a backlog of stale frames. | Dropped intermediate events; views must be able to render from a full snapshot, and they do (`syncState`). |
| Interfaces in `internal/db`, impls in `internal/repository` | Dependency inversion - nothing depends on GORM except the implementations. | Two files to touch when a method changes. |
| SQL migrations for prod, AutoMigrate for tests | Reviewable, reversible schema changes; zero-friction tests. | The migration files aren't exercised by the test suite - verify them against a scratch DB by hand. |
| Elo = Simple Multiplayer Elo | Neighbour-only comparison is defensible for N-player games and trivially explainable. | Not calibrated for wildly mixed fields. |
| PROXY protocol behind nginx | Real client IP for rate limiting, logs and traces. | Publishing 6969 would let clients forge it - the compose file warns about this. |
| Any SSH key accepted, first connection claims the username | Zero-friction signup; no passwords to store. | Username squatting; a lost key is a lost account. See §14. |

---

## 12. Invariants that bite

Ordered by how much they hurt when broken.

1. **`sessionLifecycle` must stay outermost and `releaseSession` a direct `defer`.**
   Otherwise a panic in any view kills the process for everyone.
2. **A `Rules` method must never call back into the `Engine`.** Deadlock, both locks held.
3. **Set `OverrideNextTurn`, not just `CurrentTurn`,** when you need a specific next
   actor. `advance` beats a bare `CurrentTurn`.
4. **Anything checkable belongs in `ValidateAction`.** An `AfterAction` error ends the
   game.
5. **Every subscription-holding view implements `Closer`.** goleak catches it - locally.
6. **Manager lock before lobby lock.**
7. **Never publish port 6969.**
8. **Poker: count funded seats before posting blinds.**
9. **Poker: reveal cards on `ReachedShowdown`, never on `HandComplete`.**
10. **Sizing the engine broadcaster below `len(players)+8`** hands a real player a
    closed channel and freezes their view.
11. **Placement ordering feeds Elo.** Any change to `Standings` silently changes
    ratings. `rankPlayers` needs a non-degenerate key for every group it can produce.

---

## 13. Recipes

**Add a game.**
1. `internal/game/<name>/{rules.go,state.go}` implementing `game.Rules` (+
   `PlayerLeaveHandler` if a mid-hand disconnect needs handling).
2. `internal/tui/views/game/<name>/{model,update,view}.go`, a `New(GlobalContext,
   *game.Engine) tea.Model` that binds and subscribes, plus `Close()`.
3. One entry in `catalog.All`. Registry and routes follow automatically.
4. `goleak_test.go` in the view package.

**Add a persisted field.**
1. Field on the model in `internal/db`.
2. `make migrate-create`, write **both** up and down.
3. Apply both against a scratch database by hand - the suite uses AutoMigrate and will
   not catch a broken migration.
4. Interface change in `internal/db/repository.go` if the write path changes, then the
   impl in `internal/repository`, then the mocks in `lobby_test.go` and
   `systemtest/`.

**Add a metric.** Atomic in `metrics.go`, observable instrument + callback line in
`registerAppMetrics`.

**Change poker's shape.** Match length is `poker.HandsPerMatch`; stack and blinds are
`DefaultStack`/`DefaultSmallBlind`/`DefaultBigBlind`. Chip denominations for the raise
UI are `chipDenoms` in `views/game/poker/chips.go`.

**Debug a live game.** Grafana → Tempo, filter by the `ssh.session` span and the `user`
attribute; the DB spans hang off it. Logs in Loki carry the same fields.

---

## 14. Known gaps

Not bugs - deliberate or unfinished, worth knowing before you open it to strangers.

- **No turn timer during a hand, and no SSH idle timeout.** The idle-quit tick
  explicitly skips game routes, so one player who walks away mid-hand - or one
  half-dead TCP connection - stalls the table indefinitely. Everyone else's only exit
  is esc, which forfeits. This is the single biggest griefing surface.
- **No bankroll between matches.** Chips reset to `DefaultStack` at `OnGameStart` and
  carry only across the 10 hands of one match. Deliberate.
- **Auth has no recovery.** Any key is accepted, the first connection claims the
  username, a lost key is a lost account, and usernames are squattable.
- **A busted poker player keeps their seat** for the rest of the match, folded and
  unable to act. They're told so on screen; there's no spectator mode or early exit
  short of leaving.
- **Migrations aren't covered by the test suite.** AutoMigrate builds the test schema.
- **Single node.** Restart = every live lobby and game is gone, with no warning to
  connected players.
- **Unverified operationally:** database backups, container restart behaviour under
  real load, and behaviour at a few hundred concurrent sessions.
