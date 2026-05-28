# Terminal Card Game - Testing Plan

Testing a multiplayer, server-authoritative TUI game can seem daunting because of the terminal UI and SSH layers. However, the architecture outlined in `PLAN.md` separates the **Game Engine** from the **UI**, making this highly testable.

Here is the recommended testing strategy for this specific architecture.

---

## 1. TDD vs Test-After

**Where TDD shines here:**
You should absolutely use Test-Driven Development (TDD) for the **GameRules** (e.g., `internal/game/crazyeight`) and the **TurnManager**. 
Card games are entirely logic-based and full of edge cases (e.g., "What happens if I try to play a 6 of Hearts on a 7 of Spades?", or "What happens to the turn order if two 'Skip' cards are played back-to-back?"). Writing the tests *before* the code for these rules will force you to define the FSM (Finite State Machine) clearly.

**Where Test-After (or Manual Testing) is better:**
For the **TUI** (`internal/tui`) and **SSH Server** (`internal/ssh`), TDD is frustrating. Terminal UIs involve visual spacing, layout calculations, and styling (`lipgloss`). Write the code first, verify it visually by running the app, and optionally add light tests later.

---

## 2. Unit & Structural Tests (Table-Driven Tests)

Go’s table-driven tests are perfect for the core game engine. You should test these heavily:

- **`TurnManager`**: Test `Next()`, `Reverse()`, and `SkipNext()` for 2 players, 3 players, and 5 players to ensure the math `(current + dir + total) % total` never panics.
- **`Action` Validation**: Pass a fake `GameState` and various `Action` structs to `IsValidAction()`. Check if it returns the exact expected error (e.g., `ErrNotYourTurn`, `ErrCardNotOwned`).
- **`Pile` Logic**: Test drawing from an empty deck, shuffling, and adding cards.

*Example Table-Driven Test Concept:*
```go
tests := []struct{
    name          string
    topDiscard    Card
    playedCard    Card
    expectedError error
}{
    {"Valid Suit Match", Card{Rank: 5, Suit: Hearts}, Card{Rank: 10, Suit: Hearts}, nil},
    {"Valid Rank Match", Card{Rank: 5, Suit: Hearts}, Card{Rank: 5, Suit: Spades}, nil},
    {"Valid 8 Played", Card{Rank: 5, Suit: Hearts}, Card{Rank: 8, Suit: Clubs}, nil},
    {"Invalid Play", Card{Rank: 5, Suit: Hearts}, Card{Rank: 2, Suit: Spades}, ErrInvalidCard},
}
```

---

## 3. Property-Based Testing

**Are property-based tests good here? Yes, absolutely.** 
Property-based tests (using Go's built-in `testing/quick` or a library like `gopter`) generate hundreds of random inputs to ensure "invariants" (things that must always be true) are never broken.

**Key Invariants to Property Test:**
1. **The Conservation of Cards**: After applying ANY number of random valid/invalid actions, `len(Deck) + len(Discard) + sum(len(Hands))` must ALWAYS equal the initial total card count (e.g., 52 or 104).
2. **Turn Bounds**: No combination of skips and reverses can make `CurrentTurn` `< 0` or `>= len(Players)`.
3. **Card Uniqueness**: There should never be duplicate cards in the game (unless multiple decks are intended).

Property testing is the absolute best way to catch obscure bugs in card games where a player draws exactly as the deck runs out while a shuffle is triggered.

---

## 4. Integration Tests

You should write integration tests, but you do **not** need to test the SSH layer. 

Instead, write integration tests that spin up an `Engine` in memory, attach fake clients (channels) to the `Broadcaster`, and simulate a full game loop.

**Integration Test Flow:**
1. Initialize `game.New()` with 3 dummy users.
2. Subscribe 3 Go channels to the `Broadcaster`.
3. Call `engine.Start()`.
4. Read from the channels to verify all 3 players received the `EventGameStarted` payload with their initial 7 cards.
5. Simulate Player 1 submitting an `ActionPlayCard`.
6. Verify the `Broadcaster` sends out an `EventCardPlayed` and that Player 2's turn starts.

This tests the entire pipeline (Validation -> Mutation -> Broadcasting) without having to deal with Bubble Tea or SSH mock servers.

---

## 5. Summary Recommendations

| Component | Strategy | Recommended Tools |
| :--- | :--- | :--- |
| **Game Rules / Turn Math** | **TDD** (Test First) | Standard `testing` package (Table-driven) |
| **State Invariants** | Property-Based Testing | `testing/quick` or `gopter` |
| **Engine + Broadcaster** | Integration Tests | Channels and Go routines |
| **Bubble Tea Views** | Manual Visual Testing | Human eyeballs |
| **Database Repositories** | Integration Tests | Testcontainers or a local `postgres` test DB |

**Initial Step:**
Start by writing standard unit tests for `deck/pile_test.go` and `game/turn_test.go`. Once those pass, move up to writing a test for the Crazy Eights FSM.
