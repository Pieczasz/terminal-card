---
title: "One process holds the table"
description: "Why tty.cards keeps every live card game in memory in a single Go process, and what that buys before it starts to cost."
date: 2026-08-05
draft: false
---

Most multiplayer games start by asking where the state lives. A database? Redis? An
actor per table? tty.cards answers differently: the state lives in a `map` in one
process, and the SSH session is the only transport.

That decision shapes everything else, so it is worth being honest about both halves
of it.

## What it buys

There is no serialisation boundary between a player's keypress and the rules that
answer it. A player presses `enter`, the engine takes two mutexes, the rules mutate
a `*State` in place, and every subscribed view gets the new snapshot. No JSON, no
network hop, no cache to invalidate.

That means the whole correctness argument fits in one place. `Engine` holds `e.mu`
and `state.mu` together for the entire duration of `Start`, `SubmitAction` and
`RemovePlayer`, and `Rules` methods are always called with both held. A rules
implementation can therefore treat `*State` as if it owned it — no defensive
copying, no optimistic retries, no "what if this changed underneath me".

It also means the failure modes are small enough to enumerate. A `Rules`
implementation must never call back into `Engine`, because that deadlocks. That is
the whole concurrency contract, and it is one sentence long.

## What it costs

One process means one node. There is no horizontal scaling story here, and adding
one would mean replacing the broadcaster with a real pub/sub — the code says so out
loud rather than pretending otherwise.

It also means a restart drops every game in flight. Ranked results are written to
Postgres when a hand finishes, so nothing durable is lost, but a table mid-hand is
gone. For a game whose sessions last minutes, that trade is fine. For one whose
sessions last hours, it would not be.

## The part that actually took the longest

Not the rules. Not the TUI. Disconnects.

A player closing their laptop mid-hand is the normal case, not the edge case, and
every layer has to agree about what just happened. The engine has to fold them and
pick the next actor from post-removal seat indices. The lobby has to promote a new
leader if the leaver was the host. The view has to release its broadcaster
subscription, or it parks a goroutine and holds a subscriber slot until the engine
itself closes.

Getting that last one wrong is invisible. No assertion fails; the server just
leaks a goroutine per disconnected player until something runs out. It is the kind
of bug that only shows up as a graph trending in the wrong direction three weeks
later, which is why the test suite runs `goleak.VerifyTestMain` in every package
that subscribes to anything.

---

More to come on the poker side pot logic, which is the only part of this codebase
where getting the arithmetic wrong silently moves other people's chips.
