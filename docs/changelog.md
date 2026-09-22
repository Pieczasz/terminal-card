# Changelog

Notable changes, in terms a player or an operator would care about. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). There are no
tagged releases yet; `main` is what runs.

## [Unreleased]

A hardening pass across rating, payouts, sessions, the deployment and the UI.
Most of it is invisible when everything goes right, which is exactly why it went
unnoticed for so long.

### Added

- **Delete your own account.** Profile -> `x` -> type `DELETE`. Your SSH keys and
  your ratings are erased outright; your username becomes `deleted_` plus the hex
  of your account id so the other players at your old tables keep a readable match
  history. Refused while you are seated at a table, and the session ends once it
  is done.
- **A separate limit on new accounts** - 5 per hour per client network. Playing
  on an existing account never touches that budget.
- **Explicit data retention.** Logs 14 days, traces 48 hours, metrics 30 days. It
  used to be "until the disk fills".
- Terms of Service and a Privacy Policy, in the repo and on the site
  (`docs/privacy.md`, `docs/terms.md`).
- A security policy with a private disclosure channel (`docs/SECURITY.md`).

### Documentation

- **Every document except `README.md`, `LICENSE`, `CLAUDE.md` and `AGENTS.md` now
  lives in [`docs/`](README.md)**, with `docs/README.md` as the index.
  `ARCHITECTURE.md` is `docs/architecture.md`, `READING_GUIDE.md` is
  `docs/reading-guide.md`, `ONBOARDING.md` is `docs/onboarding.md`,
  `CHANGELOG.md` is `docs/changelog.md`, `PRIVACY.md` and `TERMS.md` are
  `docs/privacy.md` and `docs/terms.md`, and
  `internal/observability/DATA.md` is `docs/data-inventory.md`.
  `docs/CONTRIBUTING.md` and `docs/SECURITY.md` keep their upper-case names so
  GitHub's own UI still links them.
- **New: [`docs/decisions.md`](decisions.md)** - one record per non-obvious
  choice, with context, consequences and the code it lives in. Thirty-seven of
  them, from "one mutex per engine" to "the odd chip goes to the lowest-sorted
  player id".
- The reading guide is now a **bottom-up** walk: leaves first, so every later
  file only uses what you already know. Per step it names the files and their
  size, the invariant to check, and the one test that teaches it.
- `docs/architecture.md` is the canonical design document and is readable top to
  bottom by someone who has never seen the code. `docs/onboarding.md` keeps the
  product story, the annotated tree and local development, and links out for
  everything else instead of repeating it.

### Changed - identity

- **Account ids are UUIDv7.** Postgres 18 generates them with `uuidv7()`; the
  Go side uses the stdlib `uuid` package. Recreate the Postgres volume: 16->18
  is a major upgrade, and the rewritten `000001` will checksum-fail otherwise.

### Changed - ratings and payouts

- **Quitting no longer helps.** A player who leaves mid-match now ranks strictly
  below everyone still at the table, and can never tie with them.
- **Tables nobody finished are not rated.** If every seat leaves, the match is
  recorded as history with no Elo change; standings for an abandoned table are
  just reverse leave order, and rating that paid the last person to quit.
- **The new-account rule is now per opponent, not per table.** A brand-new
  account (fewer than 5 ranked matches) still pays nothing to an established
  player - but the established player can still *lose* to one. Previously an alt
  at the table froze the whole result, which turned an anti-farm rule into a
  shield.
- **The anti-farm cap counts pairs of players, across all five games.** The same
  two accounts stop moving each other's rating after three ranked matches in 24
  hours, whatever game they switch to, and whoever else they seat alongside
  themselves. The old cap counted exact player sets, so rotating a third account
  through the table reset it.
- **Poker pays out correctly in five situations it previously did not:**
  - everyone folds to a bet nobody called - the uncalled part comes back;
  - an orphaned side pot no longer disappears;
  - you can no longer raise more than any opponent can call (the raise is
    refused, rather than staged and handed straight back at showdown);
  - a big blind too short to post in full no longer drags the opening bet, and
    the first legal raise, below a full blind;
  - a tripwire logs loudly if a hand ever finishes with chips unaccounted for.
- **Gin rummy defenders get the arrangement they are owed.** The defender's hand
  is now split for the lowest deadwood *after* laying off, which can be the
  difference between a loss and an undercut.
- **Hearts counts the live hand once** in the standings of a match that ended
  mid-hand - not twice, and not zero times.
- **Uno gives the last player standing their forfeit win**, including on a
  reversed table where it used to be swallowed.
- A game's identity in the database is now its slug, not its display name, so a
  game can be renamed without orphaning every rating attached to it
  (migration `000005`). Migration `000004` makes several columns `NOT NULL` that
  could previously read back as a silent zero - an unrated player at 0 Elo, a
  match attached to game 0.

### Changed - sessions and security

- **A second connection to the same account now closes the first one.** A
  half-open TCP session used to keep you out of your own seat for the whole
  90-second reconnect window.
- **Registration failures no longer say whether a name is taken or invalid.** The
  login banner had become a way to ask "does this account exist".
- **A crash in the UI now tells you so** instead of dropping the connection
  silently.
- **We log a lot less about you.** Connect and disconnect records keep only your
  network prefix (an IPv6 /64), not your address; nginx logs no client addresses
  at all; and the session trace no longer carries your address next to your
  username.
- Grafana is reachable only through an SSH tunnel to the host (`127.0.0.1:3000`),
  which CI now asserts; behind that gate it needs no login of its own.
- The deployment publishes only ports 22 and 80. Grafana stays on
  `127.0.0.1:3000` behind an SSH tunnel.
- nginx's connection and request limits key on the IPv6 /64, so one customer with
  a /64 no longer has 2^64 ways around them.
- Stats-API metrics no longer take their server label from the client's own
  `Host` header.
- The stack is sized to fit 5.5 GiB on a 12 GB / 6-core VPS, with a per-service
  limit that adds up (see the header comment in `compose.yaml`).

### Changed - tables and lobbies

- **What a match is recorded as is decided when it starts.** Reconfiguring a
  lobby after the hand ended used to rewrite the finished match's game and ranked
  flag.
- A seat taken by the idle timer now leaves the roster properly, so the table can
  reach all-ready again.
- A held seat is released when the hand ends and when the server shuts down, not
  only when its 90-second timer runs out. Previously it could keep you out of
  every other table in the meantime.

### Changed - terminal UI

- **Every screen now fits an 80x24 terminal**, and a 64x20 one. Menus, the
  leaderboard, the profile and the join browser all size themselves to the space
  they actually have instead of assuming it.
- **The spacebar works in the Hearts pass phase and in the join browser.** It
  never did; the pass phase was playable only by letting the 45-second
  auto-pass fire.
- Pressing Home while seated takes you back to your table instead of leaving your
  hand to be auto-played until the idle timer takes the seat.
- The leaderboard pages properly and ignores a response for a filter you have
  already cycled past.
- Key hints stay visible at 80x24.
- The welcome banner no longer renders your username as ASCII art (it also
  stopped being a way to fill a shared render cache).

### Fixed - internals worth knowing about

- `deck.Pile.Shuffle()` no longer returns an error nobody could trigger. Deals
  are a uniform permutation from a ChaCha8 generator seeded per call from
  `crypto/rand`.
- The turn-timeout event is now broadcast on the same lock hold that charges the
  miss, so a player whose move lands in the gap is not told they timed out after
  the miss was refunded.
- The `lobby.SessionAPI` interface is gone; views take the manager directly. It
  had one implementation and one consumer.
- Unreachable guards removed across the engine, poker and the game packages, each
  with a comment saying why it could not fire.

### Testing

- Coverage is 90% or better in every package that is not pure wiring.
- Property tests (`rapid`) for Elo's invariants, poker's "nobody loses chips
  nobody matched", and the auto-play move in four of the five games; poker has a
  deterministic equivalent.
- Eight fuzz targets, including `FuzzBestMeldSplit` and `FuzzClassifyHand`.
- `goleak` in 22 packages, fit tests per screen, and `-race` tests pinning that
  hand manipulation never aliases the pile.
- CI additionally builds the Docker image for amd64 and arm64 and validates
  `compose.yaml` and `nginx.conf`. Lint is pinned to golangci-lint v2.13.2.
