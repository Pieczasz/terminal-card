---
title: "Where the frames went"
description: "Profiling the tty.cards render path: 4,151 allocations to draw a poker table, what pprof said about them, and the one optimisation that had to be thrown away."
date: 2026-08-08
draft: false
---

The question that started this was not "is it slow". Nobody had complained. The question
was how many people fit on a VPS, and that turns out to be unanswerable without knowing
what one drawn frame costs.

So: measure a frame, profile a frame, then argue about it.

## Measuring a frame

A frame here is one `View()` call — the string a player's terminal gets after a keypress.
The benchmark builds a real model with a real engine behind it and calls `View()` in a
loop:

```
BenchmarkPokerView_Render/seats=6-8      1,036 µs/op   4,151 allocs/op
BenchmarkCrazyEightView_Render/seats=4   1,298 µs/op   4,310 allocs/op
```

Four thousand allocations to draw a card table. That is the number worth being surprised
by, not the microseconds. A millisecond of CPU per keypress is nothing; 4,151 allocations
per keypress is a GC bill that shows up later and somewhere else.

Everything below is `go test -bench=. -benchmem`, `-count=10` into `benchstat` for anything
that looked marginal, and `pprof` for the why.

## Profiling a frame

The default pprof view is bytes. That is the wrong axis when the suspicion is *count*, so:

```sh
go test -run='^$' -bench=BenchmarkPokerView_Render -memprofile=mem.out ./internal/tui/views/game/poker/
go tool pprof -sample_index=alloc_objects -top mem.out
```

`alloc_objects` counts allocations rather than weighing them, which immediately reorders
the list: the top entry by bytes was some large buffer allocated once, and the top entry by
count was a function returning a one-space string several thousand times.

`-peek` is the other half. Once a leaf looks suspicious, `-peek='PlaceHorizontal'` prints
its callers with their share, which is how you tell "this is hot because it is slow" from
"this is hot because everyone calls it".

## Finding 1: half the frame was whitespace

**48.6% of a rendered frame's allocations were lipgloss generating blank space.**

Not drawing. Not styling. Padding. `lipgloss.PlaceHorizontal` centres content by building
the pad one rune at a time, through a style renderer that can handle a styled fill
character and a custom rune — neither of which this codebase ever uses. Every framed screen
calls it three times, and every card, seat and column inside that screen calls it again.

The replacement is the same function minus what we never asked for, with padding served
from one preallocated run of spaces:

```go
var padCache = strings.Repeat(" ", 512)

func spaces(n int) string {
	if n <= len(padCache) {
		return padCache[:n]
	}
	return strings.Repeat(" ", n)
}
```

In isolation: **82,255 ns → 4,199 ns, 1,239 allocs → 2**.

Reimplementing a library function on the hottest path is the kind of thing that quietly
diverges, so it is held to lipgloss's own output by a `pgregory.net/rapid` property test
across random content, widths, heights and all nine position combinations. That earned its
keep twice. The first version was wrong, and not in a way a hand-written test would have
caught: lipgloss special-cases `Left` and `Right` in the *opposite* direction to its own
centre formula, so a "simplification" that unified them mirrored the entire layout.

## Finding 2: a card is a pure function

The second cluster was card rendering — border, pips, colours, per card, per frame. But a
rendered card depends on exactly four things: rank, suit, whether it is selected, and
whether the palette is dark. That is a few hundred possible strings, total, for the life of
the process.

```go
type cardKey struct {
	rank     deck.Rank
	suit     deck.Suit
	selected bool
	dark     bool
}

var cardCache sync.Map // cardKey -> string
```

The palette collapses to one bool because `NewTheme` is the only way to build a `Theme` and
every colour in it is a pure function of `Dark` — which is a claim, so there is a test that
fails if anyone adds a colour that isn't. A cache key that silently stops covering its
inputs is worse than no cache.

The same argument applies one level down, to the columns of an overlapping fan, and that
one mattered more: each row of each card was re-emitting its own colour escape sequence
every frame, so a seven-card hand was over a hundred style renders that never changed.

Poker went 4,151 → 930. Crazy Eights, which is mostly hand, went 4,310 → 563.

## Finding 3: the views nobody benchmarked were the worst ones

Poker and Crazy Eights got benchmarks because they felt like the hot path. The menus did
not, on the assumption that they were trivial. They were not:

| view | allocs/frame |
| --- | --- |
| Home | 17,588 |
| Profile | 12,411 |
| Leaderboard | 12,003 |
| Poker, 6 seats *(already optimised)* | 930 |

Home was nineteen times a poker table. The profile pointed at `go-figure`, which draws the
ASCII banner at the top of every screen — and **re-reads and re-parses the entire figlet
font on every call**, 85% of the frame's allocations. A banner is a pure function of its
text and the width it has to fit, so it memoises like a card does, and every menu drops to
~6,100.

The lesson is not "cache the banner". It is that the intuition about which screen was hot
was wrong by an order of magnitude, and only got corrected because someone asked why those
views had no benchmarks.

## The cache that was a vulnerability

One caller passes something the player controls:

```go
// home.go
banner := styles.RenderFigureASCII(welcomeUser, innerWidth)
```

The home screen banners *the player's own username*. Anyone with an SSH key can register
one. So the cache as written was keyed on attacker-supplied data, holding a multi-line
string per key, with no bound — a memory leak with a trivial trigger and no error anywhere
to notice it.

It is now capped:

```go
banner := renderFigureASCII(text, maxWidth)
if figureCached.Load() < maxFigureKeys {
	if _, loaded := figureCache.LoadOrStore(key, banner); !loaded {
		figureCached.Add(1)
	}
}
return banner
```

Past the cap banners still render, they just stop being remembered. The fixed screen titles
are already in by then, so the steady-state win survives and the unbounded growth does not.

A second cache went in the bin entirely over this. Caching the wrapped header and footer was
worth about 17%, but its keys were whole rendered banners — including the username one — and
nothing in the type system said headers had to stay static. Someone putting a lobby code in
a header a year from now would reintroduce the leak, and the reviewer would have no reason
to look. 17% of a render that happens on keypress is not worth a trap. Deleted.

## The optimisation that was fake

One rewrite avoided a `strings.Split` by walking lines with a callback: two allocations down
to one, clearly better in a microbenchmark.

At frame level it was neutral to slightly worse. The closure escapes to the heap, and the
callback form has to scan the string twice where the split scanned once. Reverted.

This is the whole reason the benchmarks measure `View()` and not the helpers. A helper
benchmark tells you about the helper. It does not tell you whether the helper matters, and
it will happily confirm an improvement that the surrounding code cancels out.

## Conclusion: allocations are the ceiling, not microseconds

The microseconds were never the problem. Allocation rate is, because it is what decides
whether more cores turn into more players.

Measured on the parallel render benchmark: at Go's default `GOGC=100`, going from 2 to 6
cores bought only **1.4×**. Garbage collection, not CPU, was the ceiling. At `GOGC=400` the
same jump bought **2.2×**. So compose now sets `GOGC=400` with `GOMEMLIMIT=1GiB` as the
backstop — trading memory this workload has plenty of for GC cycles it does not.

| | before | after |
| --- | --- | --- |
| Poker frame, 6 seats | 1,036 µs / 4,151 allocs | **726 µs / 930 allocs** |
| Crazy Eights frame | 1,298 µs / 4,310 allocs | **804 µs / 563 allocs** |
| Home screen | 1,353 µs / 17,588 allocs | **794 µs / 6,165 allocs** |
| Parallel render @ 6 cores | 1,917 renders/s | **4,683 renders/s** |

## Where it stops

The ~6,000 allocations left in a menu frame are lipgloss composing the outer 120×40 border
and wrapping text. That is real work with no pure-function shortcut, and it happens on
keypress, not on a tick — a browsing player costs a few thousand allocations a second, a
poker table costs fourteen thousand.

Which is the actual conclusion. The game views were the right thing to optimise. That was a
guess when the work started, and it is measured now.
