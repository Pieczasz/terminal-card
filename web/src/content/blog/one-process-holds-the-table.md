---
title: "Game state"
description: "Live game state in tty.cards lives in memory in a single Go process. What that means for the locking contract and for restarts."
date: 2026-08-05
draft: false
---

All live game state is a struct in memory in one process. No Redis, no actor per
table, no serialisation anywhere between a keypress and the rules that answer it.

## The locking contract

`Engine` holds two mutexes - its own and the state's - for the whole of `Start`,
`SubmitAction` and `RemovePlayer`:

```go
func (e *Engine) submitAction(playerID string, action Action, playerPresent bool) error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	// ...
}
```

`Rules` methods are always called with both held. That gives implementations a useful
guarantee: a `Rules` can treat `*State` as if it owned it. Mutate the slice, reassign
the map, no copying, no retry loop, no version check.

The cost is one rule: **a `Rules` implementation must never call back into `Engine`.**
That deadlocks immediately. It is the entire concurrency contract and it fits in a
sentence, which is the trade I wanted.

## Turn resolution

The cursor is settled in one place, `applyNextTurnLocked`, in this order:

1. `State.OverrideNextTurn` if the rules set one
2. otherwise advance to the next seat
3. otherwise honour whatever `State.CurrentTurn` says

Poker needs (1) constantly - after a betting round closes, the next actor is not the
next seat, it is first-to-act on the new street. So `AfterAction` writes
`OverrideNextTurn` and the engine picks it up. Crazy Eights almost never sets it and
just advances.

`ApplyAction` runs, then `AfterAction`. An error from `AfterAction` finishes the game,
because a rules set that cannot reach a consistent state should not keep dealing. That
is why anything checkable up front belongs in `ValidateAction` instead.

## Disconnects took the longest

Not the rules. Not the TUI. A player closing a laptop mid-hand is the normal case, and
every layer has to agree on what just happened.

There is an optional interface for it:

```go
type PlayerLeaveHandler interface {
	OnPlayerLeave(state *State, playerID string)
	AfterPlayerRemoved(state *State, removedIndex int)
}
```

Two hooks and not one, because poker needs both sides of the removal. `OnPlayerLeave`
folds them while the seat indices are still the old ones. `AfterPlayerRemoved` runs
once the slice has shifted, and that is where the button and blinds get reindexed and
the next actor is picked from the *new* seat numbering. Doing either in the other's
slot points a marker at the wrong player.

The failure that actually cost me an afternoon was quieter than any of that. A view
holding a broadcaster subscription has to release it:

```go
type Closer interface{ Close() }
```

Skip it and the listener goroutine parks on a channel forever and keeps a subscriber
slot until the engine itself closes. Nothing fails. No assertion notices. You find out
weeks later from a graph. Every package that subscribes to anything now runs
`goleak.VerifyTestMain`, which is the only reason I would trust it.

## What a restart costs

Every table in flight, gone. Ranked results are written to Postgres when a hand
finishes, so nothing durable is lost, but a hand in progress is not recoverable.

For sessions measured in minutes that is fine. For sessions measured in hours it would
not be, and the fix would not be a small one.
