# Contributing to Terminal Card

Thanks for contributing. These guidelines keep the project consistent and easy to extend with new card games.

## How Can I Contribute?

### Reporting Bugs

Open an issue with:

- What you were doing
- What you expected vs. what happened
- Terminal emulator, OS, and SSH client

### Suggesting Enhancements

Open an issue describing the idea, why it helps players or operators, and any implementation notes (e.g., a new game for the registry).

### Code Contributions

1. Fork and branch (`git checkout -b feature/my-awesome-feature`)
2. Make focused changes
3. Run `make test-short` (and `make lint` if you have golangci-lint)
4. Open a PR against `main` with a clear description

## Development Environment

Requires **Go 1.26+**.

```bash
cp .env.example .env
# start PostgreSQL, set DB_* in .env

export DB_DSN='postgres://postgres:PASSWORD@localhost:5432/terminal_card?sslmode=disable'
make install-tools   # golang-migrate
make migrate-up
make test-short
make build
```

### Make targets

| Target                                  | Purpose                              |
|-----------------------------------------|--------------------------------------|
| `make all`                              | fmt, fix, lint, short tests, build   |
| `make test-short`                       | Unit tests without Docker            |
| `make test` / `make test-integration`   | Full suite (testcontainers / Docker) |
| `make lint`                             | golangci-lint                        |
| `make migrate-up` / `make migrate-down` | Apply SQL migrations via `$DB_DSN`   |

### Database migrations

Schema changes live in `internal/db/migrations/` and are applied with [golang-migrate](https://github.com/golang-migrate/migrate).

- Do **not** rely on GORM AutoMigrate for production (tests may still AutoMigrate for speed).
- Add a new pair with `make migrate-create` when changing tables.
- Compose runs migrations automatically before the backend starts.

## Code style

- Pass `make fmt` and `make lint`
- Prefer table-driven tests with named subtests; use `t.Parallel()` when safe
- Wrap errors with `%w` and keep messages lowercase
- TUI: Lip Gloss for style; keep Bubble Tea models modular

## Adding a new card game

Crazy Eights and Poker (NLHE) are registered reference implementations under `internal/game/crazyeight` and `internal/game/poker`, with matching TUI packages.

### Steps

1. **Rules** - create `internal/game/<name>/` implementing `game.Rules` (and a compile-time `var _ game.Rules = (*YourRules)(nil)`). Add `var _ game.PlayerLeaveHandler = (*YourRules)(nil)` too if you handle mid-game disconnects.
2. **TUI view** - add `internal/tui/views/game/<name>/` exposing `New(router.GlobalContext, *game.Engine) tea.Model`.
3. **Register both together** - add one entry to `All` in [`internal/catalog/catalog.go`](internal/catalog/catalog.go). This is the only registration point; `cmd/server/main.go` and `internal/tui/app.go` both read from it, so rules and view can never drift apart:

   ```go
   {
       Name:  "My Game",
       Slug:  "my_game",
       Rules: func() game.Rules { return &mygamerules.Rules{} },
       View:  mygameview.New,
   },
   ```

   `internal/catalog` fails its own tests if an entry is missing a name, slug, rules factory or view, or if a slug is duplicated.
4. **Tests** - unit-test rules (and engine interactions if needed).
5. **Games table** – match recording calls `GetOrCreateGame` / `FinalizeRankedMatch` lazily; you do not need to seed a `games` row manually for development.

Lobby create options and route names (`game_<slug>`) are derived from the registry – you should not hardcode game lists.

> **Note:** Production uses SQL migrations under `internal/db/migrations/`. Tests may use GORM AutoMigrate for speed; do not rely on AutoMigrate in production.

## Pull request process

1. Keep PRs focused (one concern per PR when possible)
2. Describe *what* and *why*
3. Ensure CI passes (short tests + integration + lint)
4. Expect review feedback

Happy coding.
