# Terminal Card Game - Architecture Plan

## Overview

Server-authoritative card game engine supporting multiple game types (Crazy Eights, etc.) played over SSH terminal connections. The design emphasizes:

- **Cheat prevention**: All game state lives on the server; clients send only intents (commands).
- **Compile-time safety**: Interfaces enforce game-specific rules contracts.
- **Pluggable rules**: Strategy pattern via `GameRules` interface enables adding new games.
- **Invariant enforcement**: FSM with guard functions on all state transitions.
- **Auditability**: Monotonic sequence numbers on all actions for replay/debugging.
- **Simplicity via SSH & TUI**: Users play via standard SSH clients; the server handles everything using `charmbracelet/wish` and `bubbletea`.
- **In-Memory Pub/Sub**: WebSockets are completely unnecessary; SSH sessions communicate game state updates entirely in-memory using pure Go channels (`internal/broadcaster`).

---

## 1. Networking & TUI Architecture (Conceptual)

Because this game is played purely over SSH terminal connections, the architecture is dramatically simpler than a traditional client-server web app:

- **The "Client"**: A standard SSH connection (`ssh terminal-card.com`). No client software required.
- **The "Server"**: A Go binary running an SSH server (`charmbracelet/wish`).
- **The "UI"**: When a user connects, the server spawns a `bubbletea` program (`internal/tui/app.go`) specific to their session.
- **Communication**: Instead of WebSockets, the different `bubbletea` sessions (players in a lobby/game) communicate via the `internal/broadcaster`. When the game engine updates state, it broadcasts an `Event`, and each active player's Bubble Tea model receives it via a Go channel and updates their terminal UI.

---

## 2. Cheat Prevention Strategy (Conceptual)

```
┌─────────────────┐     SubmitAction()     ┌───────────────────┐
│ Bubble Tea View │ ───────────────────►   │   Game Engine     │
│ (Client's SSH)  │                        │   (Shared Memory) │
│                 │ ◄───────────────────   │                   │
└─────────────────┘    Event (Broadcaster) └───────────────────┘
```

**Rules:**
1. UI requests **what they want to do** (`ActionPlayCard{Card: 8♥}`), never modifies state directly.
2. Server validates against full game state before applying.
3. Server broadcasts **state snapshots** via channels to all players' TUI models.
4. Clients cannot see other players' hands (snapshot only shows hand sizes).
5. All RNG happens engine-side (`math/rand/v2` for shuffling).
6. Single mutex on `GameState` — no concurrent mutations possible.

---

## ✅ Already Implemented

The following core systems have been fully implemented in the codebase. *(Their detailed structs and code snippets have been removed from this plan as they are complete).*

- **Database (`internal/db/`)**: PostgreSQL integration with `gorm`, User profiles, and Game stats.
- **Deck Engine (`internal/deck/`)**: `Card` values, `Pile` manipulation, and deck factories.
- **Networking (`internal/ssh/`)**: Wish SSH Server and public key authentication.
- **Pub/Sub (`internal/broadcaster/`)**: In-memory channel broadcasting for game events.
- **Game Engine Core (`internal/game/`)**: The generic `Engine` state machine, `TurnManager`, `GameRules` interface, and `Action`/`Event` payloads.
- **Lobby Management (`internal/lobby/`)**: Matchmaking manager, lobby creation, code generation, and guest joining.
- **TUI Base (`internal/tui/`)**: Basic Bubble Tea application and homepage view.

---

## 🚧 Remaining Implementation Plan

These are the specific tasks, files, and features that still need to be written.

### 1. Crazy Eights Logic (`internal/game/crazyeight/`)

Currently, this folder contains only empty stubs. It needs the actual game logic implemented.

```go
// state.go
type State struct {
    Direction    int  // +1 or -1
    SkipNext     bool // an 8 was played, next player skipped
    DrawStack    int  // cumulative draw penalty from 2s stacking
    CurrentSuit  deck.Suit // current active suit
}
```

```go
// rules.go
type CrazyEightsRules struct{}

var _ game.GameRules = (*CrazyEightsRules)(nil) // compile-time check

// ... Must implement Name(), SetupInitialState(), IsValidAction(), ApplyAction()
// Checks card matching logic (suit/rank/8s).
```

### 2. Lobby to Game Transition (`internal/lobby/`)

The lobby handles grouping, but needs the function to transition players into an active game engine.

```go
// matchmaking.go

func (l *Lobby) StartGame(registry *game.Registry, b *broadcaster.Broadcaster) (*game.Engine, error) {
    // 1. Verify enough players
    // 2. Load game rules from registry
    // 3. Create engine with players and broadcaster
    // 4. Update lobby state to InGame
}
```

### 3. TUI Views (`internal/tui/`)

The homepage is done, but the application needs additional states/views:

- **Lobby View**: Show the lobby code, list of joined players, and a "Start Game" button for the leader.
- **Active Game View**: Render the game board (top discard card, player hand, hand sizes of opponents). 
- **Subscriptions**: The Bubble Tea models must listen to the `broadcaster.Broadcaster` channel to receive `game.Event` updates and trigger `tea.Cmd` re-renders.

### 4. Engine Broadcast Hooks (`internal/game/`)

In `internal/game/engine.go`, the `SubmitAction()` method has a `TODO: broadcast`.

- Hook up the `broadcaster.Broadcast(Event{...})` so the TUI actually receives the state changes.
- Ensure the `EndGame()` function writes final stats to the PostgreSQL database.
