# Gin Rummy Implementation Plan

## 1. Gin Rummy rules reference (MVP)

- **Players**: exactly 2. `MinPlayers() == MaxPlayers() == 2`.
- **Deck**: standard 52 cards, dealt fresh at the start of every hand.
- **Deal**: 10 cards each, one card flipped face-up to start the discard pile. Stock: 31 cards.
- **Turn**: draw (from stock or discard pile), then discard one card. Strict `(current+1)%2` alternation.
- **Melds**: a *set* is 3–4 cards of the same rank; a *run* is 3+ consecutive ranks of the same suit. **A-2-3 is valid; Q-K-A is not** (no wraparound).
- **Card point values**: Ace = 1, number cards 2–10 = pip value, face cards (J/Q/K) = 10.
- **Knock**: on your turn, after drawing (11 cards in hand), name one card to discard; the remaining 10 are split into melds + deadwood via `BestMeldSplit`. Legal only if deadwood ≤ 10 points.
- **Gin**: knock with 0 deadwood points. Awards a 25-point bonus (`GinBonus`).
- **Gin timing**: there is one knock action, `ActionKnock{Discard deck.Card}`. The server runs `BestMeldSplit` on the 10 cards remaining after the discard; if deadwood==0 it's Gin, 1–10 is a normal knock, otherwise illegal.
- **Layoffs**: included in MVP, but automated. After the knocker lays down their melds, the opponent's own best-split deadwood cards are checked for legal attachments to those melds (matching-rank into a set with < 4 cards, or same-suit consecutive to a run's end). All eligible cards attach automatically; the opponent's score is reduced accordingly. The knocker scores the difference in remaining deadwood, plus 25 if it was Gin.
- **Undercut**: if the opponent's post-layoff deadwood ≤ the knocker's, the opponent wins the hand and scores the difference + 25-point undercut bonus.
- **Wall / stock exhaustion**: the stock may be drawn from as long as > 2 cards remain (`WallStockSize = 2`). Once a discard brings the stock to ≤ 2, the hand ends with no score change to either player. The check happens after each discard.
- **Target score**: match ends the instant either player's `CumulativeScores[id] >= 100`. The player with the **highest** cumulative score wins (Gin Rummy scores *toward* the winner, opposite direction from Crazy Eights' ascending card-count ranking).

**Explicitly excluded from MVP**:
- Interactive layoff selection (automated instead, per §1a)
- Big Gin (all 11 melded, no discard) - deferred; provably safe (any Big-Gin-eligible hand can always shed down to ordinary Gin instead)
- First-upcard refusal rule
- Hollywood/Oklahoma scoring gimmicks, box/game/line bonuses
- Spread-based variants

**Scoring direction - explicit**: Gin Rummy scores points **to** the winner of each hand, accumulating toward 100. `Standings` must sort **descending** by cumulative score (higher is better), like poker's chip-stack ranking, not ascending like Crazy Eights' card-count ranking.

---

## 2. Data model

Hands remain exactly what they are for every other game: `player.Player.Cards []deck.Card` - no new hand-storage mechanism.

### 2.1 `internal/game/ginrummy/state.go`

```go
package ginrummy

type Phase uint8

const (
    AwaitingDraw Phase = iota
    AwaitingDiscard
    HandOver
)

const (
    KnockThreshold = 10
    GinBonus       = 25
    UndercutBonus  = 25
    TargetScore    = 100
    WallStockSize  = 2
    dealCount      = 10
)

type State struct {
    HandPhase Phase

    // FirstActor is the seat (0 or 1) that acted first in the current hand.
    // Alternates every hand. Used to park the cursor between hands.
    FirstActor int

    HandNumber int

    // CumulativeScores: higher is better. Standings sorts descending.
    CumulativeScores map[string]int

    HandComplete  bool
    MatchComplete bool

    // LastHandResult: settle-up summary for the hand that just ended.
    LastHandResult *HandResult
}

type HandResult struct {
    Knocker string  // player ID; empty on Wall

    KnockerMelds          [][]deck.Card
    KnockerDeadwood       []deck.Card
    KnockerDeadwoodPoints int

    OpponentMelds          [][]deck.Card
    OpponentDeadwood       []deck.Card
    OpponentDeadwoodPoints int
    LaidOffCards           []deck.Card

    Gin       bool
    Undercut  bool
    Wall      bool
    ScoreDelta int
    Winner     string  // player ID credited; empty on Wall
}
```

### 2.2 Actions

```go
type ActionDrawStock struct{}
func (a ActionDrawStock) Name() string { return "ginrummy.DrawStock" }

type ActionDrawDiscard struct{}
func (a ActionDrawDiscard) Name() string { return "ginrummy.DrawDiscard" }

type ActionDiscard struct {
    Card deck.Card
}
func (a ActionDiscard) Name() string { return "ginrummy.Discard" }

type ActionKnock struct {
    Discard deck.Card
}
func (a ActionKnock) Name() string { return "ginrummy.Knock" }

type ActionNextHand struct{}
func (a ActionNextHand) Name() string { return "ginrummy.NextHand" }
```

---

## 3. Rules implementation

```go
type Rules struct{}

var (
    _ game.Rules              = (*Rules)(nil)
    _ game.TurnTimeoutHandler = (*Rules)(nil)
    _ game.TurnDurationHandler = (*Rules)(nil)
    // deliberately no game.PlayerLeaveHandler: see §3.7
)

func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 2 }
func (r *Rules) InitialDeck() []deck.Card { return deck.StandardDeck() }
// InitialDealCount is zero: beginHand owns the deal for every hand of the match.
func (r *Rules) InitialDealCount() int { return 0 }
```

### 3.1 `OnGameStart` / `beginHand`

```go
func (r *Rules) OnGameStart(state *game.State) error {
    extra := &State{
        FirstActor:       state.CurrentTurn,
        CumulativeScores: make(map[string]int, len(state.Players)),
    }
    state.Extra = extra
    return r.beginHand(state, extra)
}

func (r *Rules) beginHand(state *game.State, extra *State) error {
    extra.HandNumber++
    extra.HandComplete = false
    extra.LastHandResult = nil
    extra.HandPhase = AwaitingDraw

    state.Deck = deck.New(deck.StandardDeck())
    if err := state.Deck.Shuffle(); err != nil {
        return fmt.Errorf("shuffle: %w", err)
    }
    for _, p := range state.Players {
        cards, ok := state.Deck.DrawNCards(dealCount)
        if !ok {
            return errors.New("insufficient cards to deal")
        }
        p.Cards = cards
    }
    upCard, ok := state.Deck.Draw()
    if !ok {
        return errors.New("not enough cards to start discard pile")
    }
    state.Discard = deck.New([]deck.Card{upCard})

    state.CurrentTurn = extra.FirstActor
    state.OverrideNextTurn = &extra.FirstActor
    return nil
}
```

### 3.2 `ValidateAction`

```go
func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
    extra, ok := state.Extra.(*State)
    if !ok {
        return errors.New("invalid state type")
    }

    if _, isNext := action.(ActionNextHand); isNext {
        if !extra.HandComplete {
            return errors.New("hand is still being played")
        }
        if extra.MatchComplete {
            return errors.New("match is over")
        }
        return nil
    }
    if extra.HandComplete {
        return errors.New("hand is over")
    }

    p := state.Players[state.CurrentTurn]
    switch action := action.(type) {
    case ActionDrawStock:
        if extra.HandPhase != AwaitingDraw {
            return errors.New("must discard first")
        }
        if state.Deck.IsEmpty() {
            return errors.New("stock is empty")
        }
        return nil
    case ActionDrawDiscard:
        if extra.HandPhase != AwaitingDraw {
            return errors.New("must discard first")
        }
        if _, ok := state.Discard.Peek(); !ok {
            return errors.New("discard pile is empty")
        }
        return nil
    case ActionDiscard:
        if extra.HandPhase != AwaitingDiscard {
            return errors.New("must draw first")
        }
        if !slices.Contains(p.Cards, action.Card) {
            return errors.New("you don't have that card")
        }
        return nil
    case ActionKnock:
        if extra.HandPhase != AwaitingDiscard {
            return errors.New("must draw first")
        }
        if !slices.Contains(p.Cards, action.Discard) {
            return errors.New("you don't have that card")
        }
        remaining := removeOne(p.Cards, action.Discard)
        _, _, deadwoodPoints := BestMeldSplit(remaining)
        if deadwoodPoints > KnockThreshold {
            return fmt.Errorf("deadwood %d exceeds limit %d", deadwoodPoints, KnockThreshold)
        }
        return nil
    default:
        return errors.New("unknown action")
    }
}
```

### 3.3 `ApplyAction`

```go
func (r *Rules) ApplyAction(state *game.State, action game.Action) {
    extra, ok := state.Extra.(*State)
    if !ok {
        return
    }
    p := state.Players[state.CurrentTurn]

    switch action := action.(type) {
    case ActionDrawStock:
        if drawn, ok := state.Deck.Draw(); ok {
            p.Cards = append(p.Cards, drawn)
        }
        extra.HandPhase = AwaitingDiscard
    case ActionDrawDiscard:
        if drawn, ok := state.Discard.Draw(); ok {
            p.Cards = append(p.Cards, drawn)
        }
        extra.HandPhase = AwaitingDiscard
    case ActionDiscard:
        p.Cards = removeOne(p.Cards, action.Card)
        state.Discard.AddCard(action.Card)
        extra.HandPhase = AwaitingDraw
    case ActionKnock:
        r.applyKnock(state, extra, action)
    case ActionNextHand:
        // dealt in AfterAction
    }
}

func (r *Rules) applyKnock(state *game.State, extra *State, action ActionKnock) {
    knockerID := state.Players[state.CurrentTurn].ID
    opponentID := state.Players[1-state.CurrentTurn].ID

    knockerHand := state.Players[state.CurrentTurn].Cards
    opponentHand := state.Players[1-state.CurrentTurn].Cards

    result, remaining := computeKnockOutcome(knockerID, opponentID, knockerHand, opponentHand, action.Discard)

    state.Players[state.CurrentTurn].Cards = remaining
    state.Discard = deck.New([]deck.Card{})  // clear discard pile
    extra.HandPhase = HandOver
    extra.HandComplete = true
    extra.LastHandResult = result

    if result.Wall {
        // no score change, no next-turn setup needed (matched-complete path handles it)
        return
    }

    extra.CumulativeScores[result.Winner] += result.ScoreDelta
    if handTargetReached(extra) {
        extra.MatchComplete = true
        state.OverrideNextTurn = nil
        return
    }

    nextFirst := 1 - extra.FirstActor
    extra.FirstActor = nextFirst
    state.CurrentTurn = nextFirst
    state.OverrideNextTurn = &nextFirst
}
```

### 3.4 Meld evaluation and layoffs

**Standalone pure function** (`internal/game/ginrummy/melds.go`):

```go
// BestMeldSplit partitions hand into melds (runs and sets) minimizing deadwood points.
// For a 10-11 card hand, brute-force enumeration is trivial: generate all maximal-rank-set
// and maximal-suit-run candidates, then enumerate non-overlapping combinations.
func BestMeldSplit(hand []deck.Card) (melds [][]deck.Card, deadwood []deck.Card, deadwoodPoints int) {
    // [detailed algorithm: generate candidate runs/sets, enumerate non-overlapping subsets,
    // return the split with minimum deadwood points]
}

// ApplyLayoffs extends knockerMelds with opponent deadwood cards that attach legally:
// matching-rank into a set with < 4 cards, or same-suit consecutive to a run's end.
// Repeats passes until no change, since an earlier layoff can open a new attachment point.
func ApplyLayoffs(opponentDeadwood []deck.Card, knockerMelds [][]deck.Card) (extended [][]deck.Card, remaining []deck.Card)
```

**Detailed algorithm for `BestMeldSplit`** (pseudocode-level for impl):

1. Generate all maximal rank-based sets (3-4 cards of same rank).
2. Generate all maximal suit-based runs (3+ consecutive same suit, using `rankValue` for Ace-high ordering).
3. Enumerate every non-overlapping subset (a meld partition is valid if no card appears in multiple melds).
4. For each partition, compute deadwood = hand - melds, sum deadwood points, track minimum.
5. Return the minimum-deadwood partition (ties resolved by first found, deterministic iteration order).

Time: at most a few hundred candidate melds for a 10-11 card hand, combinations are trivial (no DP needed).

---

### 3.5 `AfterAction` / `CheckWinCondition` / `Standings`

```go
func (r *Rules) AfterAction(state *game.State, action game.Action) error {
    extra, ok := state.Extra.(*State)
    if !ok {
        return errors.New("invalid state type")
    }

    if _, isNext := action.(ActionNextHand); isNext {
        return r.beginHand(state, extra)
    }

    if _, isDiscard := action.(ActionDiscard); isDiscard && state.Deck.Size() <= WallStockSize {
        extra.HandComplete = true
        extra.HandPhase = HandOver
        extra.LastHandResult = &HandResult{Wall: true}
        nextFirst := 1 - extra.FirstActor
        extra.FirstActor = nextFirst
        state.CurrentTurn = nextFirst
        state.OverrideNextTurn = &nextFirst
    }
    return nil
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
    extra, ok := state.Extra.(*State)
    return ok && extra.MatchComplete
}

func (r *Rules) Standings(state *game.State) []*player.Player {
    extra, ok := state.Extra.(*State)
    if !ok {
        return nil
    }
    standings := slices.Clone(state.Players)
    slices.SortStableFunc(standings, func(a, b *player.Player) int {
        return extra.CumulativeScores[b.ID] - extra.CumulativeScores[a.ID]  // descending: higher wins
    })
    return standings
}
```

### 3.6 Timeout handlers

```go
func (r *Rules) TimeoutAction(state *game.State) game.Action {
    extra, ok := state.Extra.(*State)
    if !ok {
        return nil
    }
    if extra.HandComplete {
        if extra.MatchComplete {
            return nil
        }
        return ActionNextHand{}
    }
    if state.CurrentTurn < 0 || state.CurrentTurn >= len(state.Players) {
        return nil
    }
    p := state.Players[state.CurrentTurn]
    switch extra.HandPhase {
    case AwaitingDraw:
        return ActionDrawStock{}  // always legal
    case AwaitingDiscard:
        _, deadwood, _ := BestMeldSplit(p.Cards)
        if len(deadwood) == 0 {
            return ActionDiscard{Card: p.Cards[0]}  // fully melded; shed arbitrarily
        }
        return ActionDiscard{Card: highestPointCard(deadwood)}
    default:
        return nil
    }
}

const HandOverTimeout = time.Minute

func (r *Rules) TurnTimeout(state *game.State) time.Duration {
    extra, ok := state.Extra.(*State)
    if !ok || !extra.HandComplete || extra.MatchComplete {
        return 0
    }
    return HandOverTimeout
}
```

### 3.7 Why no `PlayerLeaveHandler`

Traced through `Engine.RemovePlayer` (lines 502–561): for a 2-player game, any leave takes `len(state.Players)` from 2 to 1, which **unconditionally** hits:

```go
if len(e.state.Players) == 1 {
    e.state.Phase = Finished
    e.state.Winner = e.state.Players[0]
    ...
}
```

before `Rules.Standings` is ever consulted. This is exactly poker's own tested behavior: the remaining player wins regardless of their score, even mid-hand. Consequently:

1. The remaining player is declared winner (correct).
2. The abandoned hand is scored as a forfeit, not a played-out hand - `CumulativeScores` is simply left exactly as it was (the interrupted hand's deadwood is never computed or awarded).
3. `Standings` ordering is correct without special-casing (only ever contains the one remaining player).

**No custom code is needed** - the engine's own `RemovePlayer` and `standingsLocked` already produce the right behavior.

---

## 4. View/TUI plan

Gin Rummy has exactly one opponent, so the layout is the **simplest of the three new games**:

### 4.1 Reused verbatim (all of it)

- `internal/tui/components/card.go`: `RenderCard`, `SuitGlyph`
- `internal/tui/views/game/layout.go`: `RenderHand`, `RenderOpponent`, `RenderOpponentMinimal`, `RenderCardBacks`, `RenderStatus`, turn clock helpers, `RenderWaitingScreen`
- `internal/tui/views/game/state.go`: `BaseState`/`SyncBaseState` (shape matches exactly: `Hand`, `TopDiscard`, `Opponents`, `MyTurn`, `CurrentPlayer`, `TurnRemaining`)
- `internal/tui/styles/theme.go`: all existing tokens suffice (no new palette entries)
- Crazy Eights' view structure (`model.go`/`update.go`/`view.go`)

### 4.2 New work

**Hand-over screen** (`renderHandOver`), modeled on poker's technique: meld-group boxes (each group is `lg.JoinHorizontal` of `components.RenderCard`), bordered and labeled "SET"/"RUN", with deadwood underneath and any laid-off cards visually distinguished.

**Action bar** - hints for legal moves: `"s draw stock | t take discard"` during `AwaitingDraw`, `"0-9 select | enter discard | k knock"` during `AwaitingDiscard`, `"enter: deal hand N"` / `"waiting for opponent"` during `HandOver`.

**No new `Theme` tokens** - Success/Error/Accented/Selection already cover gin/undercut/headers/laid-off highlighting.

---

## 5. Files to add/change

**New - rules package**:
- `internal/game/ginrummy/state.go`
- `internal/game/ginrummy/melds.go`
- `internal/game/ginrummy/melds_test.go`
- `internal/game/ginrummy/layoffs.go`
- `internal/game/ginrummy/layoffs_test.go`
- `internal/game/ginrummy/rules.go`
- `internal/game/ginrummy/rules_test.go`
- `internal/game/ginrummy/match_test.go`

**New - TUI package**:
- `internal/tui/views/game/ginrummy/model.go`
- `internal/tui/views/game/ginrummy/update.go`
- `internal/tui/views/game/ginrummy/view.go`
- `internal/tui/views/game/ginrummy/model_test.go`
- `internal/tui/views/game/ginrummy/update_test.go`
- `internal/tui/views/game/ginrummy/view_test.go`
- `internal/tui/views/game/ginrummy/goleak_test.go`

**Changed - additive only**:
- `internal/catalog/catalog.go` - append one `Entry` to `All`, add two imports

**Unchanged**:
- `internal/catalog/catalog_test.go` - validates generically
- `internal/tui/styles/theme.go` / `palette_guard_test.go` - no changes

---

## 6. Implementation phases

- [ ] **Phase 0**: scaffolding (`state.go`, empty `Rules{}`)
- [ ] **Phase 1**: `BestMeldSplit` pure function + full unit tests (test-first, isolated)
- [ ] **Phase 2**: `ApplyLayoffs` + tests
- [ ] **Phase 3**: draw/discard turn mechanics, wall check
- [ ] **Phase 4**: knock/gin resolution + scoring
- [ ] **Phase 5**: multi-hand match loop (`ActionNextHand`, target-score termination)
- [ ] **Phase 6**: timeout/duration handlers
- [ ] **Phase 7**: TUI (`model.go` -> `update.go` -> `view.go`), `catalog.go`
- [ ] **Phase 8**: disconnect + full-engine tests, manual verification

---

## 7. Testing plan

### `melds_test.go` (mirrors `poker/evaluator_test.go`)

**Table-driven tests**:
- Pure run of 3, 4, 5+ cards (one suit) - zero deadwood.
- Pure set of 3 and 4 cards (same rank) - zero deadwood.
- Mixed hand (one run + one set + deadwood) - verify point total and card assignment.
- Fully-deadwood hand - melds empty, points = sum of pips/faces.
- Exact-gin 10-card hand (two melds covering all 10) - deadwoodPoints == 0.
- Ambiguous 4-of-a-kind: e.g., four Aces + 2♠ + 3♠ - verify algorithm picks the **points-based** optimal split, not the count-based one.
- **A-2-3 valid, Q-K-A invalid**: explicit assertion that `[Q,K,A]` same suit is **not** a run (no wraparound), returns three separate deadwood cards.
- Tie-break determinism: two equally-minimal-deadwood splits - assert a valid minimal split is returned deterministically.
- `BenchmarkBestMeldSplit`: 10- and 11-card hands, several shapes.

### `layoffs_test.go`

- Single card extends a run at one end, at the other end, extends a set to 4.
- Multi-pass requirement: card only attachable after an earlier layoff.
- Set at 4 cards - no further layoffs.
- No eligible cards - unchanged.
- Card eligible for two melds - attaches to exactly one, point reduction correct.

### `rules_test.go`

**Fixtures**: `startedTable(t)`, hand size 10, both players.

**Unit tests**:
- `ValidateAction`: draw-stock/discard rejected outside phase gates, knock-legality boundary (deadwood 10 legal, 11 illegal), card-ownership checks.
- `ApplyAction`: draw moves exactly one card, discard flips phase, knock doesn't apply until `AfterAction`.
- Knock/gin resolution: gin awards 25 to knocker, no layoffs applied even if eligible; ordinary knock (deadwood 1-10, knocker lower) awards difference after layoffs; undercut (opponent post-layoff ≤ knocker) awards to opponent + 25, result.Undercut set.
- Wall: stock at 2 after discard ends the hand, no score change; stock at 3 doesn't trigger.
- `CheckWinCondition` / `Standings`: false below target, true and descending-sorted once crossed, stable ties.
- Multi-hand: `ActionNextHand` rejected while `!HandComplete`, gated to acting seat only, `FirstActor` alternates.
- `TimeoutAction`: always legal, never auto-knocks/gins, returns `ActionNextHand{}` when `HandComplete && !MatchComplete`, `nil` when match-complete.
- Full-engine smoke/conservation: real engine, many hands, random-but-legal turns, **52-card conservation** at every step + deck rebuild sanity across hand boundaries.

### `match_test.go`

- `TestMatch_ScoresCarryIntoNextHand`: knock a hand, assert `CumulativeScores` updated, `ActionNextHand`, assert scores carried forward.
- `TestMatch_NextHandRejectedWhileHandLive` / `_FromWrongSeat`.
- `TestMatch_EndsOnceCumulativeScoreCrossesTarget`: rig hands to cross 100, assert `MatchComplete` on that instant.
- `TestStandings_LeavingForfeitsTheMatchButScoresStayAsOfLastCompletedHand`: 2-player leave mid-hand, remaining player wins outright, `CumulativeScores` unchanged from before the interrupted hand.

### Property-based tests (`pgregory.net/rapid`)

- `BestMeldSplit` invariants: melds are valid and non-overlapping, deadwood is exact, point total is correct.
- Full-engine: random draw/discard/knock sequences, 52-card conservation, `CumulativeScores` only increase by amounts `settleHand` could produce (bounds check).

### TUI

- `goleak_test.go`: `TestMain(m)` with `goleak.VerifyTestMain(m)`.
- `model_test.go`: `startedTable(t)` fixture, `syncState` correctness, hand size 10.
- `update_test.go`: action submission via key handlers, `lastActionErr` on illegal attempts.
- `view_test.go`: action-bar hints per phase, hand-over screen format, "wall" banner.

### Manual end-to-end

- `./scripts/dev-session.sh` with 2 players
- Play to knock and to gin, force a wall, disconnect mid-hand and confirm immediate match end
- `go test ./...` and `golangci-lint run`

---

## 8. Notes

- `RemoveOneMatchingCard` is critical: Gin Rummy deals unique cards (no duplicates like Uno routinely does), so a simple slice-equality delete would be fine, but the defensive loop (find-and-remove-first) is cheap and correct.
- `InitialDealCount == 0` matches poker's own design: one dealing code path for every hand, hand one included, with no asymmetry.
- No `PlayerLeaveHandler` implementation at all - engine's own `RemovePlayer` already produces the right forfeit behavior.
- Layoffs are automated as a deterministic pure function, not interactive - no player ever has a real strategic reason to decline one, so the MVP gains Gin Rummy's core mechanic cheaply without needing a new UI action type.
- Big Gin is deferred safely: any hand that qualifies for Big Gin can always shed down to an ordinary Gin (zero deadwood) instead, so no win is ever lost, only a rare bonus.
- Scoring direction is **crucial**: Gin Rummy scores toward the winner (higher is better), the opposite of Crazy Eights' ascending card-count ranking. `Standings` must sort **descending**.
