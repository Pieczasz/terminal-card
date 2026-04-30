# Terminal Card Game - Architecture Plan

## Overview

Server-authoritative card game engine supporting multiple game types (Crazy Eights, etc.) played over SSH terminal connections. The design emphasizes:

- **Cheat prevention**: All game state lives on the server; clients send only intents (commands)
- **Compile-time safety**: Interfaces enforce game-specific rules contracts
- **Pluggable rules**: Strategy pattern via `GameRules` interface enables adding new games
- **Invariant enforcement**: FSM with guard functions on all state transitions
- **Auditability**: Monotonic sequence numbers on all actions for replay/debugging

---

## 1. Deck Package (`internal/deck/`)

### Card Representation

Use value types (not pointers) — cards are small and immutable.

```go
// card.go
type Rank uint8

const (
    Ace Rank = iota + 1
    Two
    Three
    Four
    Five
    Six
    Seven
    Eight
    Nine
    Ten
    Jack
    Queen
    King
    Joker
)

type Suit uint8

const (
    Spades Suit = iota
    Hearts
    Diamonds
    Clubs
    NoSuit // for Jokers or wild cards
)

type Card struct {
    Rank Rank
    Suit Suit
}
```

**Key changes from current:**
- Export `Rank` and `Suit` types (currently unexported `rank`/`suit`)
- Fix `One` rank — use `Two` for standard deck ordering
- Add `NoSuit` for Jokers/wild cards
- `Card` is a value type, never `*Card`

### Pile (Deck/Hand/Discard)

```go
// pile.go
type Pile struct {
    cards []Card
}

func NewPile(cards []Card) *Pile
func (p *Pile) Shuffle()
func (p *Pile) Draw() (Card, bool)      // returns (card, ok)
func (p *Pile) DrawN(n int) []Card      // draw multiple
func (p *Pile) Add(cards ...Card)
func (p *Pile) AddAll(cards []Card)
func (p *Pile) Peek() (Card, bool)      // check top without removing
func (p *Pile) Size() int
func (p *Pile) IsEmpty() bool
func (p *Pile) Cards() []Card           // returns copy for inspection
func (p *Pile) Contains(card Card) bool // for validation
```

**Key changes from current:**
- Use `[]Card` (values) instead of `[]*Card` (pointers)
- Combine `CheckTop` + `PickTop` into `Peek` + `Draw` with `(Card, bool)` returns
- Add `Size()`, `IsEmpty()`, `Contains()` for rule validation
- Add `DrawN()` for games that deal multiple cards
- Use `math/rand/v2` for shuffling (acceptable for casual games; `crypto/rand` for regulated)

### Deck Factory

```go
// builder.go
func StandardDeck() []Card             // 52 cards
func StandardDeckWithJokers(n int) []Card
func MultipleDecks(count int) []Card   // for games needing 2+ decks
```

---

## 2. Game Engine (`internal/game/`)

### GameRules Interface (Pluggable Strategy)

Small interface that each game implements:

```go
// rules.go
type GameRules interface {
    Name() string
    MinPlayers() int
    MaxPlayers() int

    // Setup
    InitialDealCount() int
    SetupInitialState(state *GameState) error

    // Validation — called BEFORE ApplyMove
    IsValidAction(state *GameState, action Action) error

    // State mutation — called AFTER validation passes
    ApplyAction(state *GameState, action Action) error

    // Winning
    CheckWinCondition(state *GameState) (*player.Player, bool)

    // Post-action hook (e.g., Crazy Eights "8" effects)
    AfterAction(state *GameState, action Action) error
}
```

### Game State (Shared Across All Games)

```go
// engine.go
type GamePhase uint8

const (
    PhaseWaiting GamePhase = iota
    PhaseDealing
    PhasePlaying
    PhaseEnded
)

type GameState struct {
    mu sync.Mutex

    Phase       GamePhase
    Players     []*player.Player
    CurrentTurn int // index into Players slice

    Deck     *deck.Pile
    Discard  *deck.Pile
    Hands    map[string]*deck.Pile // keyed by player ID

    Sequence int64 // monotonic counter for action ordering

    // Game-specific state (type-asserted to concrete game state)
    Rules GameRules
    Extra any // e.g., *crazyeight.State for direction, skip, etc.
}
```

**Invariants enforced:**
- `CurrentTurn` always in `[0, len(Players))`
- Every card is in exactly one place: `Deck`, `Discard`, or a player's `Hand`
- `Phase` transitions follow valid FSM path
- Actions only accepted when `Phase == PhasePlaying`
- Only `Players[CurrentTurn]` may submit actions

### Turn Manager (Cyclic Queue)

```go
// turn.go
type TurnManager struct {
    playerCount int
    current     int
    direction   int // +1 clockwise, -1 counter-clockwise
}

func NewTurnManager(playerCount int) *TurnManager
func (tm *TurnManager) Current() int
func (tm *TurnManager) Next()
func (tm *TurnManager) Reverse()
func (tm *TurnManager) SkipNext() // for "skip" cards
```

**Behavior:**
- `Next()` wraps around: `(current + direction + playerCount) % playerCount`
- Direction can be flipped (e.g., reverse cards)
- Supports skip mechanics via a separate flag

### Game Engine (Core Loop)

```go
// engine.go
type Engine struct {
    mu          sync.Mutex
    state       *GameState
    turnManager *TurnManager
    broadcaster *broadcaster.Broadcaster
    registry    *Registry
}

func New(rules GameRules, players []*player.Player, allCards []Card) *Engine
func (e *Engine) Start() error                    // deal cards, set phase
func (e *Engine) SubmitAction(playerID string, action Action) error
func (e *Engine) processAction(action Action) error // validate → apply → broadcast
func (e *Engine) advanceTurn()
func (e *Engine) EndGame(winner *player.Player)
func (e *Engine) State() GameStateSnapshot        // read-only snapshot for clients
```

**Action processing flow (cheat prevention):**
```
Client submits Action
    ↓
1. Is it this player's turn?  → reject if not
2. Is game in Playing phase?  → reject if not
3. Does player own the cards in the action? → reject if not
4. rules.IsValidAction(state, action)?      → reject if not
5. rules.ApplyAction(state, action)         // mutate state
6. rules.AfterAction(state, action)         // handle special effects
7. rules.CheckWinCondition(state)?          → end game if true
8. advanceTurn()                            // cyclic queue
9. Broadcast event to all players
```

### Action Type

```go
// messages.go
type Action struct {
    Type   ActionType
    Cards  []Card
    Suit   deck.Suit      // for suit-picking actions
    Target string         // target player ID (for directed effects)
}

type ActionType uint8

const (
    ActionPlayCard ActionType = iota
    ActionDrawCard
    ActionPickSuit
    ActionPass
)

// Events broadcast to clients
type Event struct {
    Sequence int64
    Type     EventType
    PlayerID string
    Action   Action
    State    GameStateSnapshot // full state after action
}

type EventType uint8

const (
    EventCardPlayed EventType = iota
    EventCardDrawn
    EventSuitPicked
    EventTurnAdvanced
    EventGameEnded
)

// Read-only snapshot sent to clients (never expose full internal state)
type GameStateSnapshot struct {
    Phase         GamePhase
    CurrentPlayer string
    TopDiscard    deck.Card
    DeckSize      int
    HandSize      map[string]int // hand sizes, NOT contents
    Players       []PlayerSnapshot
    Winner        string
    Sequence      int64
}
```

### Registry (Game Type Registration)

```go
// registry.go
type Registry struct {
    mu      sync.RWMutex
    games   map[string]func() GameRules
}

func NewRegistry() *Registry
func (r *Registry) Register(name string, factory func() GameRules)
func (r *Registry) Create(name string) (GameRules, error)
func (r *Registry) List() []string
```

**Usage:**
```go
// In crazyeight package init:
func init() {
    registry.Register("crazy_eights", func() game.GameRules {
        return &CrazyEightsRules{}
    })
}
```

---

## 3. Crazy Eights Implementation (`internal/game/crazyeight/`)

### Game-Specific State

```go
// state.go
type State struct {
    Direction    int  // +1 or -1
    SkipNext     bool // an 8 was played, next player skipped
    DrawStack    int  // cumulative draw penalty from 2s stacking
    CurrentSuit  deck.Suit // current active suit (may differ from top card after suit pick)
    MustFollowSuit bool // some variants require following suit
}
```

### Rules Implementation

```go
// rules.go
type CrazyEightsRules struct{}

var _ game.GameRules = (*CrazyEightsRules)(nil) // compile-time check

func (r *CrazyEightsRules) Name() string { return "Crazy Eights" }
func (r *CrazyEightsRules) MinPlayers() int { return 2 }
func (r *CrazyEightsRules) MaxPlayers() int { return 6 }
func (r *CrazyEightsRules) InitialDealCount() int { return 7 } // 5 for 5+ players

func (r *CrazyEightsRules) IsValidAction(state *game.GameState, action game.Action) error {
    // Top of discard pile
    topCard, ok := state.Discard.Peek()
    if !ok {
        return errors.New("no cards in discard pile")
    }

    extra := state.Extra.(*State)

    switch action.Type {
    case game.ActionPlayCard:
        if len(action.Cards) != 1 {
            return errors.New("must play exactly one card")
        }
        card := action.Cards[0]

        // Check player owns this card
        hand := state.Hands[action.PlayerID]
        if !hand.Contains(card) {
            return errors.New("you don't have that card")
        }

        // Check card is playable: match rank, suit, or is an 8
        if card.Rank == deck.Eight {
            return nil // 8s are always playable
        }
        if card.Suit == extra.CurrentSuit {
            return nil
        }
        if card.Rank == topCard.Rank {
            return nil
        }
        return errors.New("card doesn't match top discard")

    case game.ActionDrawCard:
        // Allowed when player can't/won't play
        if state.Deck.IsEmpty() {
            return errors.New("deck is empty")
        }
        return nil

    case game.ActionPickSuit:
        // Only allowed after playing an 8
        // (validated in AfterAction or via state flag)
        return nil
    }

    return errors.New("unknown action")
}

func (r *CrazyEightsRules) ApplyAction(state *game.GameState, action game.Action) error {
    extra := state.Extra.(*State)

    switch action.Type {
    case game.ActionPlayCard:
        card := action.Cards[0]
        hand := state.Hands[action.PlayerID]
        // Remove from hand, add to discard
        hand.Remove(card) // needs implementation
        state.Discard.Add(card)

        // Update current suit
        if card.Rank == deck.Eight {
            // Suit will be set by pick-suit action
        } else {
            extra.CurrentSuit = card.Suit
        }

    case game.ActionDrawCard:
        drawn, ok := state.Deck.Draw()
        if !ok {
            // Reshuffle discard into deck
            r.reshuffle(state)
            drawn, ok = state.Deck.Draw()
            if !ok {
                return errors.New("no cards available to draw")
            }
        }
        state.Hands[action.PlayerID].Add(drawn)

    case game.ActionPickSuit:
        extra.CurrentSuit = action.Suit
    }

    return nil
}

func (r *CrazyEightsRules) AfterAction(state *game.GameState, action game.Action) error {
    extra := state.Extra.(*State)

    // Handle 8 effects: next player picks suit
    // Handle 2 stacking: increment draw penalty
    // Handle queen skip: skip next player
    // Handle jack reversal: flip direction
    // These depend on which variant rules you want

    return nil
}

func (r *CrazyEightsRules) CheckWinCondition(state *game.GameState) (*player.Player, bool) {
    for _, p := range state.Players {
        hand := state.Hands[p.DatabaseUser.ID]
        if hand.Size() == 0 {
            return p, true
        }
    }
    return nil, false
}

func (r *CrazyEightsRules) SetupInitialState(state *game.GameState) error {
    state.Extra = &State{
        Direction:   1,
        CurrentSuit: deck.Spades, // temporary, set after first card
    }
    return nil
}
```

---

## 4. Lobby Integration (`internal/lobby/`)

### Lobby → Game Transition

The `Lobby` should create a `game.Engine` when the leader starts the game:

```go
// matchmaking.go additions

func (l *Lobby) StartGame(registry *game.Registry) (*game.Engine, error) {
    l.mu.Lock()
    defer l.mu.Unlock()

    if l.state != Waiting {
        return nil, errors.New("lobby is not in waiting state")
    }
    if len(l.guests)+1 < l.options.cardGame.MinPlayers {
        return nil, fmt.Errorf("need at least %d players", l.options.cardGame.MinPlayers)
    }

    // Build player list: leader first, then guests
    players := append([]*player.Player{l.leader}, l.guests...)

    // Create game rules from registry
    rules, err := registry.Create(l.options.cardGame.Name)
    if err != nil {
        return nil, err
    }

    // Create deck based on game requirements
    cards := deck.StandardDeck()
    engine := game.New(rules, players, cards)

    l.state = InGame
    return engine, nil
}
```

### Lobby State Machine

```go
type state uint8

const (
    Waiting state = iota
    InGame
    Closed
)
```

**Valid transitions:**
- `Waiting → InGame`: leader starts game (enough players joined)
- `Waiting → Closed`: leader cancels, or timeout
- `InGame → Closed`: game ends

**Invariants:**
- Lobby code is unique
- Player count within `[min, max]` bounds
- Leader cannot join if already a guest
- Guest cannot join if already in lobby
- Guest cannot join if lobby is full

---

## 5. Cheat Prevention Strategy

### Server-Authoritative Architecture

```
┌──────────┐     Action Intent      ┌───────────────────┐
│  Client   │ ───────────────────►  │   Game Engine     │
│  (SSH)    │                       │   (Server)        │
│           │ ◄───────────────────  │                   │
└──────────┘     Event (Snapshot)   └───────────────────┘
```

**Rules:**
1. Client sends **what they want to do** (`ActionPlayCard{Card: 8♥}`), not state changes
2. Server validates against full game state before applying
3. Server broadcasts **state snapshots** (not raw state) to all players
4. Clients cannot see other players' hands (snapshot only shows hand sizes)
5. All RNG happens server-side (`math/rand/v2` for shuffling)
6. Single mutex on `GameState` — no concurrent mutations possible

### Validation Layers

```go
// Layer 1: Engine-level (generic)
func (e *Engine) SubmitAction(playerID string, action Action) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    // Is game running?
    if e.state.Phase != PhasePlaying {
        return ErrGameNotInProgress
    }

    // Is it this player's turn?
    currentPlayer := e.state.Players[e.state.CurrentTurn]
    if currentPlayer.DatabaseUser.ID != playerID {
        return ErrNotYourTurn
    }

    // Layer 2: Game-specific rules
    if err := e.state.Rules.IsValidAction(e.state, action); err != nil {
        return err
    }

    // Apply and broadcast
    e.processAction(action)
    return nil
}
```

### Audit Trail

```go
type ActionLog struct {
    Sequence int64
    PlayerID string
    Action   Action
    Before   GameStateSnapshot
    After    GameStateSnapshot
    Timestamp time.Time
}
```

All actions logged sequentially; enables replay, dispute resolution, and debugging.

---

## 6. File Structure

```
internal/
├── deck/
│   ├── card.go       # Card, Rank, Suit types + constants
│   ├── builder.go    # StandardDeck(), MultipleDecks()
│   └── pile.go       # Pile: Draw, Add, Shuffle, Peek, Size
├── game/
│   ├── engine.go     # Engine: Start, SubmitAction, EndGame
│   ├── rules.go      # GameRules interface
│   ├── turn.go       # TurnManager: cyclic queue
│   ├── messages.go   # Action, Event, GameStateSnapshot types
│   └── registry.go   # Registry: game type registration
└── game/crazyeight/
    ├── rules.go      # CrazyEightsRules implements GameRules
    ├── state.go      # Crazy Eights specific state (direction, skips)
    └── actions.go    # Action validation helpers
lobby/
└── matchmaking.go    # Lobby with StartGame() → creates Engine
```

---

## 7. Type Safety Summary

| Type | Purpose | Notes |
|------|---------|-------|
| `Card` | Value type (Rank + Suit) | Never `*Card` |
| `Rank` | Exported uint8 | Ace=1 through King=13, Joker=14 |
| `Suit` | Exported uint8 | Spades=0, Hearts=1, Diamonds=2, Clubs=3, NoSuit=4 |
| `Pile` | Mutable card collection | Deck, hand, discard all use this |
| `GameState` | Shared state + mutex | Single lock protects all mutations |
| `GameRules` | Interface (strategy) | Each game implements this |
| `Action` | Client intent | Validated before applied |
| `Event` | Broadcast payload | Contains snapshot, not raw state |
| `GameStateSnapshot` | Read-only client view | Hides other players' hands |
| `TurnManager` | Cyclic queue | Handles direction, skips |
| `ActionLog` | Audit trail | Monotonic sequence per game |

---

## 8. Implementation Order

1. **Deck package refactor** — fix types, add methods, export Rank/Suit
2. **GameRules interface** — define contract in `game/rules.go`
3. **TurnManager** — cyclic queue with direction support
4. **GameState + Engine** — core state machine with action processing
5. **Messages types** — Action, Event, Snapshot definitions
6. **Registry** — pluggable game registration
7. **Crazy Eights rules** — full GameRules implementation
8. **Lobby integration** — StartGame creates Engine
9. **Broadcaster integration** — Engine broadcasts Events
10. **Audit logging** — ActionLog for replay
