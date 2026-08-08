# Uno Implementation Plan

## 1. Uno rules reference (MVP scope)

**Deck (108 cards)**, 4 colors × 25 + 8 wilds:
- Each color (Red/Yellow/Green/Blue): `0` ×1, `1`–`9` ×2 each, Skip ×2, Reverse ×2, Draw Two ×2 = 25 per color × 4 = 100
- Wild ×4, Wild Draw Four ×4 = 8
- **Total: 108 cards**

**Deal**: 7 cards each. Play matches the top discard's color, number, or symbol; Wild is always legal; Wild Draw Four is always legal (MVP does not gate it on "no other playable card").

**Turn mechanics**: Skip skips the next seat. Reverse flips play direction (acts as Skip in a 2-player game). Draw Two forces the next seat to draw 2 cards and lose their turn. Wild / Wild Draw Four let the player choose the next color; Wild Draw Four additionally forces the next seat to draw 4 and lose their turn. A player with no legal play draws one card from the stock; **drawing always ends the turn**.

**Win condition**: first to empty their hand wins immediately.

**Deliberately deferred from MVP** (not implemented):
- "Must play if the card you just drew is playable" - requires a second decision point within a turn (Phase 2).
- Draw-Four challenge / bluff-calling - requires hand revelation, a new engine mechanism (Phase 2).
- Calling "Uno" with a penalty for forgetting - requires a cross-player challenge window (Phase 2).
- Stacking Draw Two/Draw Four (chaining) - niche non-official variant (optional).
- Jump-in, 7-0 rule, blank/custom wild - house variants (skip indefinitely unless requested).

**Single-hand MVP** - first to empty hand wins the game, exactly like Crazy Eights. Multi-hand-to-500 is a clean Phase 2 that reuses `internal/game/poker/`'s exact match-spanning pattern (`HandNumber`/`HandsTotal`/`MatchComplete`/`ActionNextHand`).

---

## 2. Data model

### 2.1 Deck encoding

**Decision: reuse `deck.Card{Rank, Suit}` and `deck.Pile` as-is.** Do not build a separate local Uno card type.

- `deck.Pile` is genuinely content-agnostic (shuffle, draw, reshuffle on empty).
- `components.RenderCard`/`RenderFan` render `deck.Card` directly.
- A bespoke `uno.Card` type would still need conversion to `deck.Card` at the rendering boundary anyway.

**Color mapping** - reuse the 4 existing `deck.Suit` values (aliased for readability):

```go
const (
    ColorRed    = deck.Hearts
    ColorYellow = deck.Diamonds
    ColorGreen  = deck.Clubs
    ColorBlue   = deck.Spades
    ColorWild   = deck.NoSuit
)
```

**Known limitation**: `components.suitStyle` only distinguishes **two** visual tones (red for Hearts/Diamonds, dark for Clubs/Spades), not four distinct Uno colors. This is correct for real playing cards and must not be changed. **Resolved in the view**: Uno adds a small color-tag row rendered locally by the Uno view itself, layered atop the reused card art, rather than touching the shared `suitStyle`.

**Rank mapping** - reuse existing labeled ranks where they match (`deck.Two`..`deck.Nine` = `"2"`..`"9"` via existing `pipLayout`), and append **7 new `deck.Rank` constants** after `Joker` in `internal/deck/card.go`:

```go
const (
    Zero Rank = iota + 14    // continues after Joker=13
    One
    Skip
    Reverse
    DrawTwo
    Wild
    WildDrawFour
)
```

This is additive only - no changes to existing games. Extend `internal/tui/components/card.go`'s `rankLabel` switch with cases: `Zero->"0"`, `One->"1"`, `Skip->"SK"`, `Reverse->"RV"`, `DrawTwo->"+2"`, `Wild->"W"`, `WildDrawFour->"+4"`.

### 2.2 `uno.State`

```go
// internal/game/uno/state.go
type State struct {
    CurrentColor deck.Suit // always one of ColorRed/Yellow/Green/Blue once started
    Direction    int8      // +1 (clockwise) or -1 (counterclockwise)
    Passes       int       // consecutive turns where no draw was possible
}
```

### 2.3 Actions

```go
type ActionPlayCard struct {
    Card        deck.Card
    ChosenColor deck.Suit  // required for Wild/WildDrawFour, ignored otherwise
}

func (a ActionPlayCard) Name() string { return "uno.PlayCard" }

type ActionDrawCard struct{}

func (a ActionDrawCard) Name() string { return "uno.DrawCard" }
```

No separate `ActionPickColor` - color choice folds into `ActionPlayCard.ChosenColor`.

---

## 3. Rules implementation

```go
type Rules struct{}

var (
    _ game.Rules              = (*Rules)(nil)
    _ game.PlayerLeaveHandler = (*Rules)(nil)
    _ game.TurnTimeoutHandler = (*Rules)(nil)
)

func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 10 }
func (r *Rules) InitialDeck() []deck.Card { return InitialDeck() }
func (r *Rules) InitialDealCount() int { return 7 }
```

### 3.1 `OnGameStart`

Official Uno never starts on a Wild. Redraw until a colored card surfaces:

```go
func (r *Rules) OnGameStart(state *game.State) error {
    extra := &State{Direction: 1}
    state.Extra = extra
    state.Discard = deck.New([]deck.Card{})
    
    var setAside []deck.Card
    for {
        card, ok := state.Deck.Draw()
        if !ok {
            state.Deck.AddCard(setAside...)
            return errors.New("not enough cards to start")
        }
        if !isWild(card.Rank) {
            state.Discard.AddCard(card)
            extra.CurrentColor = card.Suit
            break
        }
        setAside = append(setAside, card)
    }
    if len(setAside) > 0 {
        state.Deck.AddCard(setAside...)
        if err := state.Deck.Shuffle(); err != nil {
            return fmt.Errorf("reshuffle wilds: %w", err)
        }
    }
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
    topCard, ok := state.Discard.Peek()
    if !ok {
        return errors.New("no cards in discard")
    }

    switch a := action.(type) {
    case ActionPlayCard:
        if !slices.Contains(state.Players[state.CurrentTurn].Cards, a.Card) {
            return errors.New("you don't have that card")
        }
        if isWild(a.Card.Rank) {
            if !validColor(a.ChosenColor) {
                return errors.New("must choose a valid color")
            }
            return nil
        }
        if a.Card.Suit == extra.CurrentColor {
            return nil
        }
        if a.Card.Rank == topCard.Rank && !isWild(topCard.Rank) {
            return nil
        }
        return errors.New("card doesn't match color, number, or symbol")

    case ActionDrawCard:
        return nil  // always legal
    }
    return errors.New("unknown action")
}
```

### 3.3 `ApplyAction` and turn arithmetic

Because `TurnManager.Next()` is hardcoded `(current+1)%n`, **every** Uno action must set `state.OverrideNextTurn`, even a plain number card, once `Direction` can be `-1`:

```go
func (r *Rules) advance(state *game.State, extra *State, steps int) int {
    n := len(state.Players)
    if n == 0 {
        return 0
    }
    delta := int(extra.Direction) * steps
    return ((state.CurrentTurn + delta) % n + n) % n
}

func (r *Rules) applyReverse(state *game.State, extra *State) {
    if len(state.Players) == 2 {
        // Reverse in 2-player acts as Skip (same-seat again)
        next := state.CurrentTurn
        state.OverrideNextTurn = &next
        return
    }
    extra.Direction *= -1
    next := r.advance(state, extra, 1)
    state.OverrideNextTurn = &next
}

func (r *Rules) applyForcedDraw(state *game.State, extra *State, n int) {
    victim := r.advance(state, extra, 1)
    if !drawCardsInto(state, victim, n) {
        extra.Passes++
    }
    next := r.advance(state, extra, 2)
    state.OverrideNextTurn = &next
}

func (r *Rules) applyVoluntaryDraw(state *game.State, extra *State) {
    actor := state.CurrentTurn
    if !drawCardsInto(state, actor, 1) {
        extra.Passes++
    } else {
        extra.Passes = 0
    }
    next := r.advance(state, extra, 1)
    state.OverrideNextTurn = &next
}
```

`drawCardsInto(state, playerIdx, n) bool` draws up to `n` cards one at a time, reshuffling discard-into-stock when needed (card-conserving, like Crazy Eights' `reshuffleDiscardIntoDeck`). Returns `false` only if it draws **zero** cards (deadlock).

### 3.4 `AfterAction`, `CheckWinCondition`, `Standings`

```go
func (r *Rules) ApplyAction(state *game.State, action game.Action) {
    extra, ok := state.Extra.(*State)
    if !ok {
        return
    }
    switch a := action.(type) {
    case ActionPlayCard:
        removeOneMatchingCard(state.Players[state.CurrentTurn], a.Card)
        state.Discard.AddCard(a.Card)
        extra.Passes = 0

        if isWild(a.Card.Rank) {
            extra.CurrentColor = a.ChosenColor
        } else {
            extra.CurrentColor = a.Card.Suit
        }

        switch a.Card.Rank {
        case Skip:
            next := r.advance(state, extra, 2)
            state.OverrideNextTurn = &next
        case Reverse:
            r.applyReverse(state, extra)
        case DrawTwo:
            r.applyForcedDraw(state, extra, 2)
        case WildDrawFour:
            r.applyForcedDraw(state, extra, 4)
        default:
            next := r.advance(state, extra, 1)
            state.OverrideNextTurn = &next
        }
    case ActionDrawCard:
        r.applyVoluntaryDraw(state, extra)
    }
}

func (r *Rules) AfterAction(_ *game.State, _ game.Action) error {
    return nil  // no-op
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
    for _, p := range state.Players {
        if len(p.Cards) == 0 {
            return true
        }
    }
    if extra, ok := state.Extra.(*State); ok && len(state.Players) > 0 && extra.Passes >= len(state.Players) {
        return true
    }
    return false
}

func (r *Rules) Standings(state *game.State) []*player.Player {
    standings := slices.Clone(state.Players)
    slices.SortStableFunc(standings, func(a, b *player.Player) int {
        return len(a.Cards) - len(b.Cards)  // ascending: fewest wins
    })
    return standings
}
```

### 3.5 Optional handlers

```go
func (r *Rules) TimeoutAction(_ *game.State) game.Action {
    return ActionDrawCard{}  // always legal, never needs a color
}

func (r *Rules) OnPlayerLeave(state *game.State, playerID string) {
    for _, p := range state.Players {
        if p.ID == playerID {
            state.Deck.AddCard(p.Cards...)
            p.Cards = nil
            if err := state.Deck.Shuffle(); err != nil {
                slog.Error("uno shuffle after leave failed", "error", err, "player_id", playerID)
            }
            return
        }
    }
}

func (r *Rules) AfterPlayerRemoved(_ *game.State, _ int) {}  // no-op
```

---

## 4. View/TUI plan

### 4.1 Reused verbatim

- `internal/tui/components/card.go`: `RenderCard`, `SuitGlyph`
- `internal/tui/components/fan.go`: `RenderFan`/`FanWidth`/`CardSlotWidth`
- `internal/tui/views/game/layout.go`: `RenderHand`, `RenderOpponent`, `RenderStatus`, turn clock helpers, `RenderWaitingScreen`
- `internal/tui/views/game/state.go`: `BaseState`/`SyncBaseState`
- Crazy Eights' view structure (`model.go`/`update.go`/`view.go`)

### 4.2 New or extended

**New `Theme` fields** (`internal/tui/styles/theme.go`):

```go
UnoRed    color.Color
UnoYellow color.Color
UnoGreen  color.Color
UnoBlue   color.Color
```

Justification: the existing `SuitRed`/`SuitDark` preserve real playing-card convention and must not be repurposed; Uno genuinely needs 4 mutually distinct colors.

**New `internal/tui/views/game/uno/view.go` functions**:

- `renderColorPicker()` - 2×2 bordered-cell grid (reuses Crazy Eights' suit-picker pattern), each cell in its own theme color
- `renderCurrentColorIndicator()` - reuses Crazy Eights' `renderCurrentSuitIndicator` pattern
- `renderDirectionIndicator()` - shows "⟳ Clockwise" / "⟲ Counterclockwise"
- `renderHandColorRow()` - color tag row above the hand, one glyph per card, aligned via `components.CardSlotWidth` - keeps `layout.go` untouched

---

## 5. Files to add/change

**New:**
- `internal/game/uno/state.go` - `State`, `isWild`, `validColor` helpers
- `internal/game/uno/deck.go` - color/rank aliases, `InitialDeck()`
- `internal/game/uno/rules.go` - `Rules`, action types, all rule methods
- `internal/game/uno/rules_test.go` - full unit test suite
- `internal/tui/views/game/uno/model.go` - `Model`, `New`, `syncState`
- `internal/tui/views/game/uno/update.go` - `Update`, key handlers, `Close()`
- `internal/tui/views/game/uno/view.go` - `View()`, color picker, indicators
- `internal/tui/views/game/uno/model_test.go` - model tests
- `internal/tui/views/game/uno/goleak_test.go` - `goleak.VerifyTestMain(m)`

**Changed:**
- `internal/deck/card.go` - append 7 new `Rank` constants
- `internal/tui/components/card.go` - extend `rankLabel` switch (7 cases)
- `internal/tui/styles/theme.go` - add 4 `UnoRed/Yellow/Green/Blue` fields
- `internal/catalog/catalog.go` - append one `Entry` to `All`

**Unchanged:**
- `internal/catalog/catalog_test.go` - validates any new entry generically

---

## 6. Implementation phases

- [ ] **Phase 0**: `state.go`, `deck.go` (types only)
- [ ] **Phase 1**: core play/draw validation (numbers only)
- [ ] **Phase 2**: special cards (Skip, Reverse, Draw Two, Draw Four)
- [ ] **Phase 3**: disconnect/timeout handlers, interface assertions
- [ ] **Phase 4**: TUI (`model.go`/`update.go`/`view.go`), `rankLabel` extension, `Theme` fields, `catalog.go`
- [ ] **Phase 5**: tests, manual verification (`./scripts/dev-session.sh`)

---

## 7. Testing plan

### `internal/game/uno/rules_test.go`

**Fixtures**: `createTestState()` (single-player, hand-crafted), `createMultiplayerState(t, hands...)` (N-player, real shuffle), `cardsInPlay(state)` (conservation check).

**Unit tests** (grouped subtests):
- `TestRules_ValidateAction_PlayCard`: matching color, number, symbol, wild always, wild without color rejected, invalid mismatches
- `TestRules_ApplyAction_NumberCard`: one card removed, `CurrentColor` updated, direction-handling on Reverse
- `TestRules_ApplyAction_Skip`: advances 2 seats
- `TestRules_ApplyAction_Reverse`: flips direction (3+ players) / acts as Skip (2 players)
- `TestRules_ApplyAction_DrawTwo` / `WildDrawFour`: victim draws, is skipped, correct next actor
- `TestRules_DrawCard_Reshuffle`: empty deck reshuffles mid-resolution, conserves cards
- `TestRules_CheckWinCondition`: empty hand wins, deadlock ends hand
- `TestRules_Standings_RanksByFewestCards` / `_TiesAreStable`
- `TestRules_OnPlayerLeave_*`: cards returned, unknown player no-op
- `TestRules_TimeoutAction_AlwaysDraws`
- `TestRules_Init`: deal count, deck composition, opening-wild redraw
- `TestSmoke_FullHandConservesTheDeck`: real engine, 300+ random legal turns, 108-card conservation

### TUI tests

- `model_test.go`: `startedTable(t)` fixture, `syncState` correctness, selected-card-idx tracking
- `goleak_test.go`: `TestMain(m *testing.M)` with `goleak.VerifyTestMain(m)`

### Manual end-to-end

- `./scripts/dev-session.sh` with 2-3 players
- Play number, Skip, Reverse (confirm direction indicator), Draw Two, Wild (confirm color picker), Wild Draw Four as winning last card
- Disconnect mid-hand, confirm table stability
- `go test ./...` and `golangci-lint run` (watch `funlen`/`cyclop`/`gocognit`/`nestif`)

---

## 8. Notes

- `RemoveOneMatchingCard` mirrors Crazy Eights' defensive loop to handle duplicate cards (Uno deals two identical cards per player routinely).
- `Passes >= len(state.Players)` is the deadlock condition - exactly like Crazy Eights.
- Multi-hand-to-500 is safe to defer: `ActionNextHand`, `HandNumber`, `HandsTotal`, `MatchComplete` already exist in poker's own `state.go` and `rules.go` - a second implementation of Uno can reuse those patterns directly with zero new engine changes.
