# Hearts Implementation Plan

## 1. Hearts rules reference

- **Players**: exactly 4. `MinPlayers() == MaxPlayers() == 4`. (Hearts' pass-direction rotation, follow-suit rules, and the 26-point shoot-the-moon math all assume four hands.)
- **Deck**: standard 52 cards (`deck.StandardDeck()`), deal 13 to each player, fresh **every hand** (not one persistent deck).
- **Passing phase** (start of every hand): each player picks 3 cards to pass. Direction cycles every 4 hands: hand 1 = pass left, hand 2 = pass right, hand 3 = pass across, hand 4 = no pass, hand 5 = pass left again. "Left" = next seat in turn-rotation order (`(i+1) % n`).
- **Opening lead**: the player holding the 2♣ after passing leads the first trick and **must** lead it.
- **Follow suit**: a player must follow the suit led if they hold any card; otherwise they may play anything.
- **Hearts breaking**: hearts may not be **led** until a heart has been played (discarded) in an earlier trick - *unless* the leader's entire remaining hand is hearts (forced exception).
- **First-trick restriction**: on trick 1, no player may play a heart or the Q♠, even if void in the led suit - *unless* their whole hand is only hearts and/or Q♠.
- **Trick resolution**: highest-ranked card of the **led suit** wins (no trump). Winner leads the next trick.
- **Scoring**: each heart taken = 1 point; Q♠ = 13 points. Lower cumulative score is better.
- **Shoot the moon**: if one player takes all 26 points (all 13 hearts + Q♠), that player scores **0** and every other player is charged **26** instead.
- **Match end**: play continues hand after hand until at least one player's cumulative score reaches or exceeds **100**. The player with the **lowest** cumulative score wins.

**Explicitly excluded from MVP**:
- Jack of Diamonds bonus / Omnibus variants
- Shoot-the-moon alternate scoring (shooter subtracts 26)
- 3-player or 5-player variants
- Optional passing or configurable pass direction
- Reconnect/rejoin grace windows after disconnect (see §3.7)

**Disconnect policy**: any disconnect at any point ends the match immediately with no shorthanded continuation (Hearts' rules are only defined at exactly 4 players).

---

## 2. Data model

### 2.1 `internal/game/hearts/state.go`

```go
package hearts

type Stage uint8

const (
    StagePassing Stage = iota
    StageTrickPlay
    StageHandOver
)

type PassDirection uint8

const (
    PassLeft PassDirection = iota
    PassRight
    PassAcross
    PassNone
)

const (
    playerCount        = 4
    cardsPerHand        = 13
    cardsToPass         = 3
    penaltyPointsTotal  = 26
    DefaultTargetScore  = 100
    PassTurnTimeout     = 45 * time.Second
    HandOverTurnTimeout = time.Minute
)

var (
    twoOfClubs    = deck.Card{Rank: deck.Two, Suit: deck.Clubs}
    queenOfSpades = deck.Card{Rank: deck.Queen, Suit: deck.Spades}
)

type State struct {
    Stage Stage

    PassDirection PassDirection
    PendingPasses map[string][]deck.Card  // staged 3 cards, already removed from hand
    Passed        map[string]bool         // has submitted ActionPassCards this hand

    LedSuit      deck.Suit
    TrickCards   map[string]deck.Card    // partial mid-trick
    TrickLeader  int
    HeartsBroken bool
    TricksPlayed int  // 0..13 in the current hand

    HandPoints       map[string]int  // points taken THIS hand
    CumulativeScores map[string]int  // total across the match
    HandNumber       int
    DealerIndex      int
    TargetScore      int
    HandComplete  bool
    MatchComplete bool

    LastTrickWinner string  // playerID
}
```

### 2.2 Actions

```go
type ActionPassCards struct {
    Cards []deck.Card  // exactly 3, all owned, not yet passed
}

func (a ActionPassCards) Name() string { return "hearts.PassCards" }

type ActionPlayCard struct {
    Card deck.Card
}

func (a ActionPlayCard) Name() string { return "hearts.PlayCard" }

type ActionNextHand struct{}

func (a ActionNextHand) Name() string { return "hearts.NextHand" }
```

---

## 3. Rules implementation

```go
type Rules struct{}

var (
    _ game.Rules               = (*Rules)(nil)
    _ game.PlayerLeaveHandler  = (*Rules)(nil)
    _ game.TurnTimeoutHandler  = (*Rules)(nil)
    _ game.TurnDurationHandler = (*Rules)(nil)
)

func (r *Rules) MinPlayers() int { return playerCount }
func (r *Rules) MaxPlayers() int { return playerCount }
func (r *Rules) InitialDeck() []deck.Card { return deck.StandardDeck() }
func (r *Rules) InitialDealCount() int { return 0 }  // beginHand owns the deal
```

### 3.1 `OnGameStart` / `beginHand`

```go
func (r *Rules) OnGameStart(state *game.State) error {
    n := len(state.Players)
    if n != playerCount {
        return fmt.Errorf("hearts requires exactly %d players, got %d", playerCount, n)
    }
    extra := &State{
        CumulativeScores: make(map[string]int, n),
        HandPoints:       make(map[string]int, n),
        TrickCards:       make(map[string]deck.Card, n),
        TargetScore:      DefaultTargetScore,
    }
    for _, p := range state.Players {
        extra.CumulativeScores[p.ID] = 0
    }
    state.Extra = extra
    return r.beginHand(state, extra, state.CurrentTurn)
}

func (r *Rules) beginHand(state *game.State, extra *State, dealer int) error {
    resetHandState(extra)
    extra.HandNumber++
    extra.DealerIndex = dealer

    state.Deck = deck.New(deck.StandardDeck())
    if err := state.Deck.Shuffle(); err != nil {
        return fmt.Errorf("shuffle deck: %w", err)
    }
    for _, p := range state.Players {
        cards, ok := state.Deck.DrawNCards(cardsPerHand)
        if !ok {
            return errors.New("not enough cards to deal")
        }
        p.Cards = cards
    }

    extra.PassDirection = PassDirection((extra.HandNumber - 1) % 4)
    if extra.PassDirection == PassNone {
        extra.Stage = StageTrickPlay
        leader := findTwoOfClubs(state)
        state.CurrentTurn = leader
        state.OverrideNextTurn = &leader
        return nil
    }

    extra.Stage = StagePassing
    extra.PendingPasses = make(map[string][]deck.Card, playerCount)
    extra.Passed = make(map[string]bool, playerCount)
    state.CurrentTurn = dealer
    state.OverrideNextTurn = &dealer
    return nil
}
```

### 3.2 Card-passing mechanics (the key design call)

**Decision: reuse the engine's single-`CurrentTurn`-cursor, single-action-at-a-time model. Cycle through all 4 seats, each submitting `ActionPassCards`; stage the cards immediately (remove from hand); apply all 4 atomically once the 4th lands.**

Why: zero engine changes; no information leak (cards are staged until all 4 have passed); symmetric with existing turn-timeout machinery; the fiction is transparent to the player (they experience "it's my turn, pick 3 cards").

```go
func (r *Rules) afterPass(state *game.State, extra *State) error {
    if len(extra.Passed) < len(state.Players) {
        next := nextUnpassedSeat(state, extra, state.CurrentTurn)
        state.OverrideNextTurn = &next
        return nil
    }
    applyAllPasses(state, extra)
    extra.Stage = StageTrickPlay
    extra.PendingPasses = nil
    extra.Passed = nil
    leader := findTwoOfClubs(state)
    state.CurrentTurn = leader
    state.OverrideNextTurn = &leader
    return nil
}

func applyAllPasses(state *game.State, extra *State) {
    n := len(state.Players)
    for i, p := range state.Players {
        recipient := passRecipient(i, extra.PassDirection, n)
        state.Players[recipient].Cards = append(state.Players[recipient].Cards, extra.PendingPasses[p.ID]...)
    }
}

func passRecipient(from int, dir PassDirection, n int) int {
    switch dir {
    case PassLeft:
        return (from + 1) % n
    case PassRight:
        return (from - 1 + n) % n
    case PassAcross:
        return (from + 2) % n
    default:  // PassNone
        return from
    }
}
```

### 3.3 `ValidateAction`

```go
func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
    extra, ok := state.Extra.(*State)
    if !ok {
        return errors.New("invalid state type")
    }

    if _, isNextHand := action.(ActionNextHand); isNextHand {
        if extra.Stage != StageHandOver {
            return errors.New("the hand is still being played")
        }
        if extra.MatchComplete {
            return errors.New("the match is over")
        }
        return nil
    }

    switch extra.Stage {
    case StagePassing:
        return validatePass(state, extra, action)
    case StageTrickPlay:
        return validatePlay(state, extra, action)
    default:
        return errors.New("hand is over")
    }
}

func validatePass(state *game.State, extra *State, action game.Action) error {
    a, ok := action.(ActionPassCards)
    if !ok {
        return errors.New("must pass cards during passing phase")
    }
    if len(a.Cards) != cardsToPass {
        return fmt.Errorf("must pass exactly %d cards", cardsToPass)
    }
    p := state.Players[state.CurrentTurn]
    if extra.Passed[p.ID] {
        return errors.New("you already passed this hand")
    }
    seen := make(map[deck.Card]bool)
    for _, c := range a.Cards {
        if seen[c] {
            return errors.New("duplicate card in pass")
        }
        seen[c] = true
        if !slices.Contains(p.Cards, c) {
            return errors.New("you don't have that card")
        }
    }
    return nil
}

func validatePlay(state *game.State, extra *State, action game.Action) error {
    a, ok := action.(ActionPlayCard)
    if !ok {
        return errors.New("must play a card during trick play")
    }
    p := state.Players[state.CurrentTurn]
    if !slices.Contains(p.Cards, a.Card) {
        return errors.New("you don't have that card")
    }

    leading := len(extra.TrickCards) == 0
    if leading && extra.TricksPlayed == 0 && a.Card != twoOfClubs {
        return errors.New("must lead the 2 of clubs on trick 1")
    }
    if leading && a.Card.Suit == deck.Hearts && !extra.HeartsBroken && !onlyHearts(p.Cards) {
        return errors.New("hearts have not been broken yet")
    }
    if !leading && a.Card.Suit != extra.LedSuit && handHasSuit(p.Cards, extra.LedSuit) {
        return errors.New("must follow suit")
    }
    if extra.TricksPlayed == 0 && isPenaltyCard(a.Card) && hasNonPenaltyCard(p.Cards) {
        return errors.New("cannot play a point card on trick 1")
    }
    return nil
}
```

### 3.4 `ApplyAction` / `AfterAction`

```go
func (r *Rules) ApplyAction(state *game.State, action game.Action) {
    extra, ok := state.Extra.(*State)
    if !ok {
        return
    }
    switch a := action.(type) {
    case ActionPassCards:
        p := state.Players[state.CurrentTurn]
        p.Cards = removeCards(p.Cards, a.Cards)
        extra.PendingPasses[p.ID] = a.Cards
        extra.Passed[p.ID] = true
    case ActionPlayCard:
        p := state.Players[state.CurrentTurn]
        p.Cards = removeCard(p.Cards, a.Card)
        if len(extra.TrickCards) == 0 {
            extra.LedSuit = a.Card.Suit
            extra.TrickLeader = state.CurrentTurn
        }
        extra.TrickCards[p.ID] = a.Card
        if a.Card.Suit == deck.Hearts {
            extra.HeartsBroken = true
        }
    case ActionNextHand:
        // dealt in AfterAction
    }
}

func (r *Rules) AfterAction(state *game.State, action game.Action) error {
    extra, ok := state.Extra.(*State)
    if !ok {
        return errors.New("invalid state type")
    }
    switch action.(type) {
    case ActionPassCards:
        return r.afterPass(state, extra)
    case ActionPlayCard:
        return r.afterPlay(state, extra)
    case ActionNextHand:
        return r.beginHand(state, extra, state.CurrentTurn)
    }
    return nil
}

func (r *Rules) afterPlay(state *game.State, extra *State) error {
    if len(extra.TrickCards) < len(state.Players) {
        return nil  // mid-trick, engine's default +1 rotation is correct
    }

    winnerID, winnerSeat := trickWinner(state, extra)
    extra.HandPoints[winnerID] += trickPoints(extra.TrickCards)
    extra.LastTrickWinner = winnerID
    extra.TricksPlayed++
    extra.TrickCards = make(map[string]deck.Card, len(state.Players))
    extra.LedSuit = deck.NoSuit

    if extra.TricksPlayed < cardsPerHand {
        state.CurrentTurn = winnerSeat
        state.OverrideNextTurn = &winnerSeat
        return nil
    }

    scoreHand(extra, state.Players)
    extra.Stage = StageHandOver
    extra.HandComplete = true

    if handTargetReached(extra) {
        extra.MatchComplete = true
        state.OverrideNextTurn = nil
        return nil
    }

    nextDealer := (extra.DealerIndex + 1) % len(state.Players)
    state.CurrentTurn = nextDealer
    state.OverrideNextTurn = &nextDealer
    return nil
}
```

### 3.5 Trick and scoring helpers

```go
func rankValue(r deck.Rank) int {
    if r == deck.Ace {
        return 14
    }
    return int(r) + 1
}

func trickWinner(state *game.State, extra *State) (string, int) {
    bestValue := -1
    winnerSeat := extra.TrickLeader
    for seat, p := range state.Players {
        card, ok := extra.TrickCards[p.ID]
        if !ok || card.Suit != extra.LedSuit {
            continue
        }
        if v := rankValue(card.Rank); v > bestValue {
            bestValue = v
            winnerSeat = seat
        }
    }
    return state.Players[winnerSeat].ID, winnerSeat
}

func trickPoints(cards map[string]deck.Card) int {
    pts := 0
    for _, c := range cards {
        if c.Suit == deck.Hearts {
            pts++
        }
        if c == queenOfSpades {
            pts += 13
        }
    }
    return pts
}

func scoreHand(extra *State, players []*player.Player) {
    shooterID := ""
    for _, p := range players {
        if extra.HandPoints[p.ID] == penaltyPointsTotal {
            shooterID = p.ID
            break
        }
    }
    if shooterID != "" {
        for _, p := range players {
            if p.ID != shooterID {
                extra.CumulativeScores[p.ID] += penaltyPointsTotal
            }
        }
        return
    }
    for _, p := range players {
        extra.CumulativeScores[p.ID] += extra.HandPoints[p.ID]
    }
}

func handTargetReached(extra *State) bool {
    for _, s := range extra.CumulativeScores {
        if s >= extra.TargetScore {
            return true
        }
    }
    return false
}
```

### 3.6 `CheckWinCondition` / `Standings` / Timeout handlers

```go
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
        return extra.CumulativeScores[a.ID] - extra.CumulativeScores[b.ID]  // ascending: lower wins
    })
    return standings
}

func (r *Rules) TimeoutAction(state *game.State) game.Action {
    extra, ok := state.Extra.(*State)
    if !ok {
        return nil
    }
    switch extra.Stage {
    case StageHandOver:
        if extra.MatchComplete {
            return nil
        }
        return ActionNextHand{}
    case StagePassing:
        p := state.Players[state.CurrentTurn]
        return ActionPassCards{Cards: threeLowestCards(p.Cards)}
    case StageTrickPlay:
        p := state.Players[state.CurrentTurn]
        if card, ok := firstLegalCard(state, extra, p); ok {
            return ActionPlayCard{Card: card}
        }
        return nil
    default:
        return nil
    }
}

func (r *Rules) TurnTimeout(state *game.State) time.Duration {
    extra, ok := state.Extra.(*State)
    if !ok {
        return 0
    }
    switch extra.Stage {
    case StagePassing:
        return PassTurnTimeout
    case StageHandOver:
        if extra.MatchComplete {
            return 0
        }
        return HandOverTurnTimeout
    default:
        return 0  // trick play uses engine default 30s
    }
}

func (r *Rules) OnPlayerLeave(state *game.State, playerID string) {
    if extra, ok := state.Extra.(*State); ok {
        extra.MatchComplete = true
    }
}

func (r *Rules) AfterPlayerRemoved(_ *game.State, _ int) {}
```

---

## 4. View/TUI plan

### 4.1 Reused verbatim

- `internal/tui/components/card.go`: `RenderCard`, `SuitGlyph`
- `internal/tui/components/fan.go`: `RenderFan`/`FanWidth`/`CardSlotWidth`
- `internal/tui/views/game/layout.go`: `RenderOpponent`, `RenderCardBacks`, `RenderStatus`, `RenderWaitingScreen`, turn clock helpers
- `internal/tui/views/game/state.go`: `BaseState`/`SyncBaseState` (note: `TopDiscard` is not read for Hearts - confirm in `model.go` comment)
- `internal/tui/styles/theme.go`: all existing tokens suffice (no new palette fields needed)

### 4.2 New work (no precedent in codebase)

**Multi-select hand UI**: extract `RenderFanCore` from `components/fan.go`, add sibling `RenderFanMulti` for multi-select case. Same change at layout level: add `RenderHandMulti` alongside untouched `RenderHand`. Both 4-line additions on top of existing shared rendering cores - backwards compatible, Crazy Eights/Poker unaffected.

**Trick-pile rendering** - 4-way cross of played cards (hero + 3 opponents):

```go
func (m *Model) renderTrickArea() string {
    hero := m.renderTrickSlot(m.bound.PlayerID())
    left := m.renderTrickSlot(m.opponentID(0))
    top := m.renderTrickSlot(m.opponentID(1))
    right := m.renderTrickSlot(m.opponentID(2))
    return lg.JoinVertical(lg.Center,
        top,
        lg.JoinHorizontal(lg.Center, left, "  ", right),
        hero,
    )
}
```

**Hand-score-history screen** - modeled on poker's `renderHandOver` technique: titled section listing each player's hand points + cumulative score, with Gin/Undercut/Wall/Match-Over banners.

**Hearts-broken indicator** - reuses Crazy Eights' `renderCurrentSuitIndicator` pattern:

```go
func (m *Model) renderHeartsBrokenIndicator() string {
    if m.heartsBroken {
        return m.global.Theme.Warning.Render("♥ Hearts: broken")
    }
    return m.global.Theme.Muted.Render("♥ Hearts: not yet broken")
}
```

---

## 5. Files to add/change

**New - rules package**:
- `internal/game/hearts/state.go`
- `internal/game/hearts/rules.go`
- `internal/game/hearts/trick.go` (trick-winner, scoring, pass-helpers)
- `internal/game/hearts/rules_test.go`
- `internal/game/hearts/trick_test.go`
- `internal/game/hearts/match_test.go`

**New - TUI package**:
- `internal/tui/views/game/hearts/model.go`
- `internal/tui/views/game/hearts/update.go`
- `internal/tui/views/game/hearts/view.go`
- `internal/tui/views/game/hearts/model_test.go`
- `internal/tui/views/game/hearts/view_test.go`
- `internal/tui/views/game/hearts/goleak_test.go`

**Changed - additive only, backwards compatible**:
- `internal/tui/components/fan.go` - extract `renderFanCore`, add `RenderFanMulti`
- `internal/tui/views/game/layout.go` - add `RenderHandMulti` alongside untouched `RenderHand`
- `internal/catalog/catalog.go` - append one `Entry` to `All`, add two imports

**Unchanged**:
- `internal/catalog/catalog_test.go` - validates generically
- `internal/tui/styles/theme.go` - no new tokens needed
- `internal/tui/styles/palette_guard_test.go` - no exemptions needed

---

## 6. Implementation phases

- [ ] **Phase 0**: state/deck scaffolding (`state.go`, empty `Rules{}`)
- [ ] **Phase 1**: pass-phase mechanics (staging, atomic apply on 4th, `AfterPass`)
- [ ] **Phase 2**: trick-play + follow-suit validation, hearts-broken timing
- [ ] **Phase 3**: scoring + shoot-the-moon + multi-hand loop
- [ ] **Phase 4**: TUI (`model.go` -> `update.go` -> `view.go`), `RenderFanMulti`/`RenderHandMulti`, `catalog.go`
- [ ] **Phase 5**: disconnect/timeout handlers, verify `RemovePlayer` end-to-end
- [ ] **Phase 6**: tests + manual verification

---

## 7. Testing plan

### `rules_test.go`

**Fixtures**: `createTestState()`, `createMultiplayerState(t, hands...)`, `cardsInPlay(state)`.

**Key tests**:
- `TestRules_ValidateAction_Pass_*`: card count, ownership, duplicates, second-pass rejection
- `TestRules_AfterAction_Pass_RotatesThroughAllFourThenApplies`: partial-pass state, atomic 4th-player apply
- `TestRules_ValidateAction_Play_MustLeadTwoOfClubsOnTrickOne`
- `TestRules_ValidateAction_Play_MustFollowSuitWhenHoldingIt`
- `TestRules_ValidateAction_Play_HeartsCannotBeLedBeforeBroken`
- `TestRules_ApplyAction_Play_DiscardingAHeartBreaksHearts`: off-suit heart discard sets the flag
- `TestRules_ApplyAction_Play_TrickWinnerIsHighestOfLedSuit`: non-led cards ignored
- `TestRules_ApplyAction_Play_WinnerLeadsNextTrick`: `OverrideNextTurn` set to winner
- `TestRules_CheckWinCondition_TrueOnlyWhenMatchComplete`
- `TestRules_Standings_RanksByAscendingCumulativeScore` / `_TiesAreStable`
- `TestRules_TimeoutAction_*`: passing returns 3 legal cards, trick-play returns a legal card, hand-over returns `ActionNextHand{}` unless match-complete
- `TestRules_OnPlayerLeave_EndsTheMatchImmediately` (mid-passing, mid-trick)
- `TestSmoke_FullHandConservesTheDeck`: real engine, 13 tricks, 52-card conservation at every step

### `match_test.go`

- `TestMatch_PassDirectionCyclesAcrossManyHands`: hand 1-8 check the sequence
- `TestMatch_NoPassHandSkipsPassingPhaseEntirely`: hand 4 goes straight to trick-play with 2♣ leader
- `TestMatch_CumulativeScoresCarryIntoNextHand`: total after hand 1 + hand 2's points = cumulative after hand 2
- `TestMatch_DealerRotatesBetweenHands`
- `TestMatch_NextHandRejectedWhileHandIsLive`
- `TestMatch_EndsOnceCumulativeScoreCrossesTarget`: 100-point boundary
- `TestMatch_ShootTheMoon_ShooterScoresZeroOthersGain26`

### TUI

- `goleak_test.go`: `TestMain(m)` with `goleak.VerifyTestMain(m)`
- `model_test.go`: `startedTable(t)` fixture, `syncState` correctness
- `view_test.go`: substring assertions on `View()` output, hand-over screen format

### Manual end-to-end

- `./scripts/dev-session.sh` with 4 players
- Play through: pass-left hand, no-pass hand, hearts breaking, forced first-trick point-card, full match to 100
- Disconnect mid-passing, disconnect mid-trick - confirm immediate match end
- `go test ./...` and `golangci-lint run` (watch `funlen`/`cyclop`/`gocognit`/`nestif`)

---

## 8. Notes

- `rankValue` is duplicated here from poker's `evaluator.go` - the third trick-taking game arriving later is the trigger to promote it to `internal/deck.RankValue()`.
- Pass-direction alternates every hand: hand 1 (PassLeft), hand 2 (PassRight), hand 3 (PassAcross), hand 4 (PassNone), hand 5 (PassLeft), etc. - computed fresh in `beginHand` from `(HandNumber - 1) % 4`.
- Any player disconnect triggers `extra.MatchComplete = true`, which forces `Engine.RemovePlayer`'s own "1 player remains" path to finish the game immediately. No shorthanded continuation exists.
- Multi-select card UI is built on top of a backwards-compatible `RenderFanCore` extraction - `RenderFan` and `RenderHand` never change their signatures or behavior.
