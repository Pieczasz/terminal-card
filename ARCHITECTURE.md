# Architecture

Current as of the `fix/review-hardening` tree. Every section names the file or
symbol a reader should open; where the code already explains *why*, this document
points at the comment instead of paraphrasing it.

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
                 |
                 v
   +-----------------------------------------+
   |  nginx (stream proxy)  internal/config/nginx.conf
   |  limit_conn 8 / $client_net   PROXY protocol on
   +-----------------------------------------+
                 |  PROXY header (real client IP)
                 v
   +-----------------------------------------+
   |  backend :6969 (Go)      cmd/server      |
   |   Wish SSH + Bubble Tea                  |
   |   lobby.Manager (in memory)              |
   |   game.Engine per match                  |
   |   stats API :6970       internal/httpapi |
   +-----------------------------------------+
          |                        |
          v                        v
   +---------------+   +---------------------------+
   | Postgres 18   |   | Alloy -> Loki/Tempo/Prom  |
   | users, keys   |   |        -> Grafana         |
   | games, ranks  |   +---------------------------+
   | matches       |
   +---------------+
```

**Published ports are 22 and 80, both on nginx.** Grafana is the single exception
and binds `127.0.0.1:3000`. Everything else - Postgres, Alloy, Loki, Tempo,
Prometheus, and the backend's `6969`/`6970` - is `expose`-only on the compose
network. The comment above `proxy.ports` in `compose.yaml` states the reason:
publishing `6969` lets a client forge the PROXY header, and publishing `6970`
lets it forge `X-Forwarded-For`, which `API_TRUST_PROXY=true` tells the backend
to believe. Local bare-ssh needs `PROXY_PROTOCOL=false` (see
`scripts/dev-session.sh`).

### Defence in depth

| Layer | Limit | Where |
|---|---|---|
| nginx (stream) | 8 conns / client network | `nginx.conf` `limit_conn ssh_addr 8` |
| nginx (http) | 2 r/s + burst 10 on `/api/` | `nginx.conf` `limit_req_zone $client_net` |
| listener | `MAX_CONNECTIONS` (default 1000) | `netutil.LimitListener` |
| auth | `RATE_LIMIT_CONNECTIONS` / window, per network | `ssh.rateLimitAuth` |
| **registration** | **5 / hour / network, new accounts only** | `ssh.allowRegistration`, `registrationLimit` |
| stats API | `API_REQUESTS_PER_MINUTE` (120) per network | `httpapi.withRateLimit` |
| channels | 2 sessions / SSH connection | `maxSessionsPerConnection` |
| account | one live slot / user; **second Connect displaces and closes the first** | `ssh.SessionTracker.Connect` |
| lobby join | 10 / s / player | `Manager.joinLimiter` |
| TUI idle | quit after 5 min idle (not mid-game) | `router.Router` |
| SSH idle | Wish idle timeout (30 min) | `wish.WithIdleTimeout` |

Every network limit keys on `ratelimit.NetKey`, which collapses IPv6 to its /64 -
one customer is routinely handed 2^64 addresses, so keying on the full address is
keying on nothing. nginx does the same thing with a `$client_net` map, duplicated
across the `stream` and `http` contexts because the two cannot share one.

`NetKey` itself is total: an address it cannot parse comes back verbatim. The
**fail-closed** decision is one level up, in `ssh.netKeyFor`, which returns
`ok=false` for a nil address or a `SplitHostPort` error; `rateLimitAuth` and
`allowRegistration` both refuse on `!ok` rather than sharing one bucket for every
unkeyable peer.

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
2. `LoadOrRegisterUser` - first connection claims the SSH username. It takes an
   `allowRegister func() bool` and consults it **only** on the `user == nil`
   branch, so a returning player never spends the registration budget
3. `tracker.Connect(userID, s)` -> generation. A second session for the same
   account **displaces** the first *and closes its connection*, outside the
   tracker lock (a wedged peer must not hold every other account's `Connect`
   behind it). Capacity -> `ErrServerFull`
4. Build TUI; stash `sessionState{user, model, gen, span, traceCtx}` in a
   session-keyed map - never on `s.Context()`, which is per-**connection** and
   shared by every channel
5. On teardown: `releaseSession` - if `tracker.Owns(user, gen)`, then
   `DisconnectPlayer` + `Release`; a displaced gen touches neither seat nor slot

Auth sentinels (`db.ErrUsernameTaken`, …) live in `internal/db/errors.go`. SSH
depends on the `db` contract package, not `internal/repository`.

**Registration refusal is deliberately uninformative.** `mapRegisterError` folds
`db.ErrUsernameTaken` and `db.ErrInvalidUsername` into one
`ssh.ErrNameUnavailable`; the comment above it says why (a distinguishable
message turns the login banner into a "does this account exist" oracle). The
rate-limit refusal is its own string, `ErrTooManyRegistrations`.

**A TUI panic reaches the player.** Two layers, and they cover different cases.
`recoverSession` is a **direct** `defer` in the outermost middleware (a
`recover()` inside a function called *by* a deferred function returns nil) and
`wish.Fatalf`s the notice. But nothing panics *out of* bubbletea - it owns the
screen - so the primary path is `reportingModel`, which wraps `Init`/`Update`/
`View`, and `notifySessionPanic` writes `panicNotice` to **`s.Stderr()`**.
Both record the panic as a metric and leave the lobby cleanly.

### 3.3 TUI - `internal/tui`

`tui.Model` builds `router.Router` with `GlobalContext`:

- `User`, `UserRepository`
- `LobbyManager *lobby.Manager` - the whole manager. The old `lobby.SessionAPI`
  interface is **gone**: it had exactly one implementation and one consumer, so
  it bought nothing and had to be edited every time a view needed a method
- `GameRegistry`, `SessionCtx`, `Width`, `Height`, `Theme`

There is **no `MatchRepository` on the TUI**. Persistence is owned by
`lobby.Manager` after a game ends.

Initial route: `ResumePlayer` -> mid-game reconnect lands back at the table;
otherwise home / waiting lobby.

Game routes come from `catalog.All` via `router.GameRoute(slug)` -> `"game_<slug>"`.
`internal/game` knows nothing about routes.

**A seated player cannot navigate away from their table.** `RouteHome`,
`RouteLobbyCreate` and `RouteLobbyJoin` all call `FindLobbyByPlayer` first and
mount the lobby view instead (`internal/tui/app.go`). Navigating away
unsubscribes but keeps the seat, so without this the engine would auto-play the
player's turns until the idle timer took it.

#### The layout budget - `internal/tui/styles/common.go`

Every screen must fit at 64x20 (`styles.MinWidth` x `styles.MinHeight`), 80x24
and 120x50. That holds because one unexported function owns the arithmetic and
both the renderer and the budget query call it:

- `layoutHeights(w, h, header, footer)` wraps the header and footer to
  `innerWidth` **before** measuring them, and returns
  `hContent = max(innerHeight - hHeader - hFooter, 0)`.
- `AvailableContentHeight(...)` = that, minus `opticalPadding` (the two blank
  lines the renderer appends).
- `Theme.RenderMainLayout(...)` calls the same `layoutHeights`, so the promise
  and the render cannot drift. The comment there names the two bugs this fixed:
  header/footer measured unwrapped in one place and wrapped in the other, and
  `opticalPadding` never being deducted.
- `TitleHeightBudget(h)` caps the figlet title at a fifth of the box interior. At
  20 rows that is 2 lines, which makes `RenderFigureASCII(text, maxW, maxH)` fall
  back to plain text and hand the rows back to content.

Views reach it through `views.RenderScreen` / `views.ScreenContentHeight`
(`internal/tui/views/common.go`). Lists size themselves from it rather than
assuming rows - see `leaderboard.pageLayout`, which derives `rowsPerPage` from
`ScreenContentHeight` and clamps between `minRowsPerPage` and `maxRowsPerPage`.

`RenderFigureASCII` memoises on its text in a `sync.Map` capped at
`maxFigureKeys`. Nothing player-controlled may be banner text: the home screen
used to figlet the username, which let any account mint cache entries until the
cap filled and every real title re-parsed the font each frame.

#### Key names, not key literals

`tea.KeyPressMsg.String()` normalises the spacebar to `"space"`. Matching the
literal `" "` is a silent no-op, and it silently broke two screens: the Hearts
pass phase (playable only by waiting out the 45-second auto-pass) and the join
browser's select. Both now match `"space"`, with a comment saying so
(`internal/tui/views/game/hearts/update.go`, `internal/tui/views/lobby/join.go`).

#### Account deletion - Profile

`x` on the Profile screen asks for confirmation; typing `DELETE` performs it.
Refused while the player is seated at a table. The view calls
`db.UserRepository.DeleteAccount(ctx, userID)` and then ends the session -
nothing can authenticate as that account afterwards. See §5.3.

### 3.4 Lobby - `internal/lobby`

- `WithCardGame(name string)` - the in-memory domain id is the game **display
  name**, which is the `game.Registry` key. The persisted identity is the slug,
  resolved at game start (§5.1)
- `BrowseLobbies(player, BrowseFilter)` - public waiting tables, Elo-distance
  sorted, cached ~2s, hard-capped at `DefaultBrowseLimit`
- Ready -> `startGameLocked` -> `registry.Create(name)` -> `game.NewEngine` ->
  `watchGameLocked` -> broadcast `EventGameStarted` with the engine

**The finalize snapshot is taken when the game starts, not when it ends.**
`watchGameLocked` builds the `finalizeRequest{lobbyCode, game, isRanked,
startedAt}` at subscribe time; its comment says why (by the time the game ends
the lobby may have reopened and been reconfigured, and the result would be
written under the new ranked flag, the new game and the next hand's start time).
`startGameLocked` sets `l.startedAt` immediately before, for the same reason.

**That watcher subscribes even with no match repository.** It is also the only
consumer of `EventPlayerIdle`, and skipping it would leave an idle-removed seat
on the roster while the engine no longer holds it - a table that can never reach
all-ready again. `finalizeFinishedGame` already no-ops without a repo.

Mid-game disconnect: `DisconnectPlayer` arms **`DisconnectGrace` (90s)** via
`disconnectGrace` (`disconnect.go`: `pending` timers -> `expiring` claim ->
`LeaveLobby`). `ResumePlayer` cancels a pending leave, or returns the seat on
takeover with no pending leave. Waiting-lobby seats and shutdown still leave
immediately. `expireLeave` moves pending -> expiring under the manager lock so a
reconnect cannot resume a seat about to vanish.

**The hold is released at two more points, both in `manager.go`:**

- `releaseHeldSeats`, called from `Lobby.releaseFinishedGame` - the hold only
  makes sense mid-hand. Once the table is Waiting again, a still-armed timer
  keeps the player out of every other table, and this one unable to reach
  all-ready, for up to 90s.
- `BeginShutdown` - those timers would fire long after the drain, leaving the
  lobby un-removed and its engine un-closed. The player is not coming back to a
  process that is exiting.

### 3.5 Play - `internal/game`

```
key -> view.Update -> BoundEngine.Submit(action)
                         |
                         v
                 Engine.SubmitAction (e.mu held)
                   ValidateAction -> ApplyAction -> AfterAction
                   CheckWinCondition / applyNextTurnLocked
                   broadcast Event*
```

Views sync with `Session.Sync(fn)` -> `BoundEngine.Frame(fn func(*State))`: one lock
hold for snapshot, own hand, turn clock, and live `*State` (Extra is unredacted -
copy what you keep).

### 3.6 Finish - `Manager.finalizeFinishedGame`

Lobby watcher sees `EventGameEnded` -> `requestFinalize` (using the snapshot
taken at game start) -> **`Manager.finalizeFinishedGame`** (`finalize.go`):

1. `registerFinalizer` (refuse if shutdown already stopped new finalizers)
2. `StandingsWithPlaces` (ties share place when rules implement `StandingScorer`)
3. The rating gate (§5.2)
4. `FinalizeRankedMatch` or `RecordCasualMatch`

Then `releaseFinishedGame` reopens the lobby so settings work without another
ready - and releases any seats still held for a reconnect.

---

## 4. Contracts (the ones that bite)

### 4.1 Rules <-> Engine

`Rules` in `internal/game/rules.go`. The engine holds **exactly one** mutex
(`Engine.mu`, `engine.go`) for `Start` / `SubmitAction` / `RemovePlayer` /
`Frame`. `game.State` has **no** lock of its own; it is protected entirely by the
engine's.

- Rules must **never** call back into `Engine` (deadlock). This holds
  structurally: `Rules` methods receive only `*State`, which carries no engine
  handle. Do not add one
- Rules may mutate `*State` freely under the engine lock
- `ValidateAction` rejects cleanly; `ApplyAction` / `AfterAction` errors finish the
  game as `EndReasonRulesError` (may be half-applied) -> **unrated** persistence
- Optional: `PlayerLeaveHandler`, `TurnTimeoutHandler`, `TurnDurationHandler`,
  `StandingScorer`

Per-game state: `State.Extra` (`*crazyeight.State`, `*poker.State`, …).

### 4.2 Turn cursor and clock - `applyNextTurnLocked`

1. `OverrideNextTurn` if set (then clear)
2. else advance if requested
3. else honour `CurrentTurn`

Then clamp (a leave handler can compute an index against the pre-removal seat
count) and arm the clock: `DefaultTurnTimeout` 30s, `MaxMissedTurns` 3
(`turnclock.go`). No `TurnTimeoutHandler` -> no clock at all; there is nothing
safe to play for an absent player, so they get no clock rather than a silent
removal. `TurnDurationHandler` stretches a particular turn (hearts: 45s for the
pass phase, a minute for the between-hands prompt; poker and gin rummy stretch
their deal the same way). Returning zero keeps the default and cannot resurrect a
clock `WithTurnTimeout` disabled.

`turnSeq` fences stale timers: every cursor change invalidates timers already in
flight, and an auto-play carries the generation it was computed for. Only
**accepted** actions clear a player's miss count - a move the rules reject does
not, or spamming garbage would dodge removal forever.

`EventTurnTimedOut` is broadcast **inside** `resolveTurnTimeout`'s lock hold, on
the same hold that charged the miss. Outside it, a player whose action lands in
the gap gets the miss refunded while the "timed out" they disproved still ships.

`OverrideNextTurn` also keeps a last-seat-standing hand open (poker HU all-in
leave still contests the pot).

### 4.3 BoundEngine

`Bind(engine, playerID)` - default safe path: submit as self, hand is yours,
`Frame` is one coherent read. Not a capability boundary: `Engine()` still reaches
whole-table state (poker showdown needs every seat). The value is that the safe
path is the default, so reaching past it is a visible detour and redaction
becomes the view's stated job (`buildSeats`).

### 4.4 Session / view baseline

Every game view embeds `gameview.Session` (`internal/tui/views/game/session.go`):
subscribe, `HandleFrame`, cursor, `IdleRemoved`, leave, `Close`
(`router.Closer`). Skipping `Close` parks a listener and burns a subscriber slot.
Read per-game state through `Sync`'s `extra` callback; read seat order, names and
stock size through `BaseState`. Anything kept past the lock must be copied
(`maps.Clone`, `HandResult.Clone`), never aliased.

### 4.5 Subscriptions

`broadcaster.Broadcaster[T]` is latest-wins. `Subscribe` returns
`ErrAtCapacity` / `ErrClosed` rather than a pre-closed channel - a closed channel
is indistinguishable from a finished game. Engines size `len(players)+8` for
finalize watcher + reconnect overlap.

### 4.6 Lock order

Manager (`m.mu`) **then** lobby (`l.mu`), then engine (`Engine.mu`). Never
invert. Browse cache invalidation uses an atomic dirty flag specifically so a
lobby can mark it while holding its own lock without reaching for the manager's.

### 4.7 Shared deck helpers - `internal/deck`

`RemoveOne`/`RemoveEach` never alias. Three rank questions that must not be
swapped: `RankValue` (Ace high, 14 - poker and hearts), `RunOrder` (Ace low,
courts distinct - gin rummy runs), `PipValue` (courts count 10 - deadwood). All
three answer **0** outside Ace..King, including the Joker: no deck here deals
one, and 0 loses loudly instead of quietly tying the ace. Standard ranks are
1-based so a zero `deck.Card` is detectably empty; Uno's extra ranks sit at 20+.
`IsSuit` is the guard for "a card that lets the player name a suit" - an Eight, a
Wild - and refuses `NoSuit`, the zero value and client garbage alike.

`Pile.Shuffle()` **returns nothing.** It seeds a `math/rand/v2` ChaCha8 generator
once per call from `crypto/rand`, which since Go 1.24 cannot fail (it aborts the
process instead), so the error every caller used to plumb through was an
unreachable branch dressed as resilience. Several such guards were removed across
the engine and the rules packages in the same pass; each site carries a comment
saying why it was unreachable.

---

## 5. Persistence, Elo, and the anti-farm rules

Interfaces in `internal/db`; GORM in `internal/repository`.

### 5.1 Game identity is the slug

`games.slug` is what a rating hangs off - `catalog.Entry.Slug`, the same value
the TUI derives routes from. `games.name` is a display column, refreshed on every
write. Renaming a game in `internal/catalog` used to create a second `games` row
and orphan every ranking on the first (migration `000005_game_slug`, which also
drops the old unique index on `name`).

The two halves travel together as `db.GameRef{Slug, Name}`:

```
catalog.Entry{Name, Slug}
  -> game.Module{Name, Slug, Factory}     registry keyed by Name
  -> lobby options.cardGame (Name)
  -> at start: db.GameRef{Slug: mod.Slug, Name: l.options.cardGame}
  -> repository getOrCreateGame: upsert ON CONFLICT (slug), name refreshed
```

Migration `000004_not_null_scalars` pinned the columns whose Go field is a plain
scalar - `rankings.elo`, `matches.game_id`, `public_keys.user_id`,
`public_keys.name` - because a NULL scans into the zero value there and reads
back as data rather than as a missing value. `TestSchemaNullabilityMatchesStructs`
derives its list from the GORM structs, so a new scalar field fails CI until it
is pinned.

### 5.2 What is rated, and what is only recorded

`persistFinishedMatch` (`internal/lobby/finalize.go`):

```go
rated := req.isRanked && !m.isShuttingDown() &&
    reason != game.EndReasonRulesError && reason != game.EndReasonAbandoned
```

Three ways a ranked match is written without Elo, and the comment gives one
reason for each: a deploy decided who was left holding cards, not play; a
half-applied rules error must not move the ladder; and an **abandoned** table -
every seat left - has standings that are reverse leave order, so rating it pays
the last to quit. `EndReasonForfeit` (last player standing) **is** rated.
`unratedReason` logs which of the three it was. An abandoned match with no
standings writes nothing at all.

**Leavers rank strictly below seated players.** `standingsLocked` appends
`LeftPlayers` after the seats the rules placed (reverse leave order among
themselves), and `placesLocked` guards the tie branch with `!left[p.ID]`: a
leaver's `StandingScore` was measured against a state they are no longer in, so
tying it with a seated player would turn a rage-quit into a rated draw.

### 5.3 Ranked finalize - `FinalizeRankedMatch`, one transaction

1. `lockPairing` - `pg_advisory_xact_lock` **per seat**, in user-id order. Not
   per exact participant set: the cap below is per *pair*, and two different sets
   can share one. Ranking row locks are per `(user, game)`, so the same accounts
   finalizing Poker and Hearts at the same moment would otherwise both read an
   undamped count. Sorted order is what stops two overlapping tables deadlocking
2. `seedRankingRows` - revive soft-deleted ranking rows, then upsert seeds
3. `fetchRankings` - `SELECT … FOR UPDATE`, ordered by user id
4. Provisional rule + pairing damping + `elo.Calculate`
5. Write match + participants + places + deltas

**Provisional is a per-pair rule, not a per-account flag.** `elo.Player` carries
`Provisional` (set by the repository from `MatchesPlayed < provisionalMatches`,
5; a missing ranking row counts as provisional). `elo.Calculate` scores adjacent
pairs, and `unpaidAgainstProvisional` clamps the **established** side's gain to
zero on a mixed pair while letting losses through unchanged. Identity is a free
SSH keypair, so a fresh 1500 is free to mint - but if an established player could
not lose to one either, seating an alt would freeze a rating in place and the
anti-farm rule would become a shield. The provisional side always moves, so it
converges on real games. This deliberately breaks Elo's zero-sum property for
that pair; the comment says the unfarmable ladder is worth more.

**The anti-farm cap is per pair, across every game.**
`repeatedPairCountLast24h` counts, over ranked non-deleted matches in the last
24 hours, the highest co-occurrence of any *pair* of players at this table -
not the exact participant set. `{A,B}`, `{A,B,C}` and `{A,B,D}` are three
different sets, so a cap on set repeats handed each its own budget and two
accounts could farm each other forever by rotating a third alt through. The
window spans every game for the same reason: switching game does not make it
legitimate. At `maxSamePairingPerDay` (3) the table is **damped** - nobody's
`elo` is written, only `matches_played`, which is what still lets a provisional
account graduate.

### 5.4 Casual

`RecordCasualMatch` - history only, no Elo, game row created on first sight.

### 5.5 Erasure - `DeleteAccount`

`db.UserRepository.DeleteAccount(ctx, userID)`, implemented by
`eraseUserLocked` (`internal/repository/user.go`), one transaction:

- `public_keys` and `rankings` are **hard**-deleted (`Unscoped`). A soft-deleted
  key row would keep its unique fingerprint and lock the returning player out of
  registering again; a soft-deleted ranking still holds the `(user_id, game_id)`
  primary key. With the keys gone, nothing can authenticate as that account.
- The `users` row **survives, anonymised**: `username` becomes
  `db.AnonymisedUsername(userID)` (`deleted_` plus 32 hex digits of the UUID,
  which has to satisfy `varchar(40)` and the CHECK that allows either a chosen
  name of at most 16 characters or that exact `deleted_` form) and
  `last_seen_at` becomes NULL. It is updated by column, not `Save`d from a loaded
  struct, because the save hooks would walk the associations the transaction just
  deleted and write them back.
- `match_participants` is untouched. Those rows belong to the *other* players at
  those tables, and their history has to keep resolving to a name.
- The leaderboard cache is cleared wholesale afterwards - a five-minute TTL is
  five minutes of an erased name on screen.

The TUI path is Profile -> `x` -> type `DELETE`, refused while seated, session
ends after.

---

## 6. Catalog - single registration point

`internal/catalog/catalog.go` `All` is the only place a game is declared. Each
entry carries rules factory **and** TUI view constructor. `cmd/server` builds the
registry; `internal/tui/app.go` registers routes. Missing field / duplicate slug
fails `catalog_test.go`. Copying an entry and changing only the rules still
compiles, so keep the pair in lockstep by hand.

| Field | Consumer |
|---|---|
| `Module.Name` | registry key, lobby option, `db.Game.Name` (display) |
| `Slug` | TUI route `game_<slug>`, **and `games.slug`, the persisted identity** |

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

Seat identity inside `internal/game` is the scalars on `game.Player` (`UserID`,
`Name`, `Ratings`), never a `*db.User`.

Lobby file split:

| File | Role |
|---|---|
| `manager.go` | maps, join/new/kick, grace release, shutdown |
| `lobby.go` | roster, ready, start, watcher -> `requestFinalize` |
| `finalize.go` | persist finished matches, rating gate |
| `disconnect.go` | mid-game grace state machine |
| `browse.go` | public list + `GameNames` |

---

## 8. Stats API - `internal/httpapi`

`/v1/stats` (players online, hands in play, tables open) and
`/v1/leaderboard?limit=N` (1-200, default 5). Mounted by `cmd/server/main.go` on
`API_PORT` (6970), reached only through nginx's `/api/` location.

Deliberately narrow: no writes, no auth, no per-user data, nothing the TUI
leaderboard does not already show any visitor. That is what makes it safe
unauthenticated. Live counts come from `ssh.SessionTracker.Count` and
`lobby.Manager.Stats`, so `SetupServer` takes an optional `Tracker` for sharing.

`API_TRUST_PROXY` makes the limiter read `X-Forwarded-For`. It **defaults to
false** and compose opts in explicitly: a directly exposed listener that trusts
the header can be evaded by forging it, so the unsafe direction has to be chosen.
nginx sets it from `$remote_addr`, not `$proxy_add_x_forwarded_for`, so a client
cannot prepend its own value.

`otelhttp.WithServerName("stats-api")` pins the metric's server label. Without
it, otelhttp labels every request metric with the client's own `Host` header -
the same unbounded-cardinality hole `routeSpanName` closes for span names.

---

## 9. Observability and retention

The app **pushes** OTLP to Alloy; Alloy scrapes only the host. Nothing pulls the
Go process, and there is no `/metrics` and no pprof endpoint.

| Signal | Store | Retention | Set in |
|---|---|---|---|
| Logs | Loki | **14 days** | `internal/config/loki/loki.yaml` `retention_period: 336h` + compactor |
| Traces | Tempo | **48 hours** | `internal/config/tempo/tempo.yaml` `block_retention: 48h` |
| Metrics | Prometheus | **30 days** (8 GB cap) | `compose.yaml` `--storage.tsdb.retention.time=30d` |
| Container stdout | Docker json-file | 3 x 10 MB | `compose.yaml` `x-logging` |

Two decisions worth knowing before you add an attribute:

- **The session span carries no client address.** It has `client_version` and the
  terminal size at start, and `user` at the end. The comment in `startSession`
  says why: the span already carries the username, and joining the two is exactly
  the record a trace store should not hold for 48 hours.
- **Connect and disconnect log `client_net`, the /64, not the IP.** The full
  `remote_addr` survives only on WARN/ERROR paths, where abuse investigation
  needs it. nginx contributes nothing: the `stream` block is `access_log off` and
  the `http` block uses a `log_format privacy` with no `$remote_addr` and no
  User-Agent.

`internal/observability/metrics_test.go` collects every instrument and fails if
any attribute key falls outside a fixed allow-list, so "metrics carry no personal
data" is enforced rather than asserted.

Per-field detail, for whoever has to answer a data question:
[`internal/observability/DATA.md`](internal/observability/DATA.md).

---

## 10. Invariants that bite

1. Never publish `:6969` (PROXY trust) or `:6970` (`API_TRUST_PROXY`).
2. `sessionLifecycle` outermost; recover is a **direct** defer; the bubbletea
   path is `reportingModel` -> `s.Stderr()`.
3. Displaced session must not `LeaveLobby` / free the tracker slot. `Connect`
   closes the displaced connection **outside** the tracker lock.
4. Mid-game drop -> `DisconnectPlayer`, not `LeaveLobby`. The hold is released on
   hand end and on shutdown, not only by its timer.
5. Finalize on Manager, from the snapshot taken at game **start**. RulesError,
   abandoned, and shutdown -> history without Elo.
6. Soft-deleted rankings must be revived before seed or finalize aborts.
7. Advisory locks are per seat, sorted; the 24h damp count is per pair, across
   games.
8. Views that subscribe implement `Closer`; router/`releaseSession` call it.
9. Anything kept after `Frame`/`Sync` must be copied, not aliased.
10. Lock order: manager -> lobby -> engine. `State` has no lock.
11. Catalog entry = rules + view; the slug is persisted, so changing one migrates
    data.
12. Accept-loop / API failure still runs `drainServer`.
13. Every screen fits 64x20; measure with `AvailableContentHeight`, never assume
    rows.
14. Chips are conserved. Poker's `checkChipConservation` is the tripwire, not the
    enforcement - see §11.

---

## 11. Poker money invariants - `internal/game/poker`

The one place in the codebase where a bug is a *payout*, so it is defended in
layers. Read the comments on each of these; they state the failure mode.

| Concern | Symbol | File |
|---|---|---|
| Nobody wins chips nobody matched (fold-out) | `awardUncontested` | `streets.go` |
| Same, at showdown | `refundUncalled` | `streets.go` |
| Dead money from folded players rides with the last live layer | `buildSidePots` (`orphan`) | `streets.go` |
| A hand that cannot be played out unwinds | `refundContributions` | `streets.go` |
| A raise past what any opponent can call is refused, not staged | `largestCallableBet` / `validateRaiseTo` | `rules.go` |
| A blind too short to post keeps the full bring-in | `beginHand` (`extra.CurrentBet = extra.BigBlind`) | `rules.go` |
| The tripwire: stacks + pool must equal the hand's starting total | `checkChipConservation` | `rules.go` |

`checkChipConservation` **logs**; it does not panic or refuse. By the time it
fires the hand is already closed out, so the value is the log line, not a
recovery. It also checks `MainPool != 0` separately, because `chipsInPlay`
counts the pool - a hand that ends without paying a pot out would otherwise
balance, and the next `resetForHand` would quietly zero the stranded chips.

The property test behind it is `TestChipsAreConservedAcrossRandomHands`
(`streets_test.go`), whose ledger asserts "nobody loses chips nobody matched"
over rapid-generated hands.

---

## 12. Doc map

| Doc | Use |
|---|---|
| [`README.md`](README.md) | What it is, how to play, how to run |
| [`READING_GUIDE.md`](READING_GUIDE.md) | Ordered file tour, data-flow maps, tooling |
| [`ONBOARDING.md`](ONBOARDING.md) | Product context, patterns, run/test |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to add a game / test norms / PR norms |
| [`SECURITY.md`](SECURITY.md) | Disclosure, scope, deployment hardening |
| [`CLAUDE.md`](CLAUDE.md) | Short ops brief for agents |
| [`internal/observability/DATA.md`](internal/observability/DATA.md) | Per-field data inventory |

---

## 13. Reading the call graph

The repo is indexed by GitNexus (`.gitnexus/`, local, not committed). Two seams
matter more than any single query result:

**There is no static path from `BoundEngine.Submit` to `finalizeFinishedGame`.**
The engine broadcasts `EventGameEnded`; a different goroutine
(`handleBroadcasterEvents`) picks it up. Searching only the call graph will miss
it.

```
Lobby.startGameLocked -> watchGameLocked -> handleBroadcasterEvents
  -> requestFinalize -> Manager.finalizeFinishedGame
      -> registerFinalizer / shutdownCtx
      -> persistFinishedMatch -> Engine.StandingsWithPlaces
                              -> rating gate
                              -> recordFinishedMatch -> MatchRepository
```

**Generation fencing and grace fencing are two different mechanisms that must
agree.** `Owns`/`Release` on the SSH side, `pending` -> `expiring` on the lobby
side. A reconnect race is what happens when they disagree.

```
releaseSession
  -> SessionTracker.Owns / Release
  -> Manager.DisconnectPlayer
      -> disconnectGrace.arm (mid-game)
      -> OR LeaveLobby / expireLeave -> Lobby.notifyEngineAndBroadcast
          -> Engine.RemovePlayer -> Manager.RemoveLobby (empty table)
```

Re-index after a large merge:

```bash
node .gitnexus/run.cjs analyze --index-only
```

Absent edges across channels and timers mean "dynamic hop", not "dead code".
