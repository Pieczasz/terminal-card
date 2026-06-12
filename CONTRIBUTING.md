# Contributing to Terminal Card

First off, thank you for considering contributing to Terminal Card! We appreciate your time and effort. 

The following is a set of guidelines for contributing to this project. These are mostly guidelines, not strict rules. Use your best judgment, and feel free to propose changes to this document in a pull request.

## How Can I Contribute?

### Reporting Bugs
If you find a bug, please create an issue detailing:
- What you were doing when the bug occurred.
- What you expected to happen.
- What actually happened.
- Details about your terminal emulator, OS, and SSH client.

### Suggesting Enhancements
Enhancement suggestions are highly encouraged! Please create an issue explaining:
- The new feature or improvement you'd like to see.
- Why it would be useful.
- Any ideas on how to implement it (if applicable).
- E.g., proposing new card games to be added to the registry!

### Code Contributions
If you want to contribute code:
1. Fork the repository.
2. Create a new branch for your feature or bugfix (`git checkout -b feature/my-awesome-feature`).
3. Make your changes.
4. Run tests and formatting tools (see Development Environment below).
5. Commit your changes with clear, descriptive commit messages.
6. Push to your fork and submit a Pull Request against the `main` branch.

## Development Environment Setup

This project uses Go 1.26. We use a `Makefile` to simplify common development tasks.

### Quick Commands

- `make all`: Runs formatting, linting, tests, and builds the project.
- `make build`: Compiles the server binary into `bin/server`.
- `make test`: Runs all unit tests.
- `make lint`: Runs `golangci-lint` (you will need to install it: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`).
- `make fmt`: Runs `go fmt` on the codebase.
- `make fix`: Runs `go fix` on the codebase.

### Code Style and Guidelines

- **Go Standards:** We follow standard Go conventions. Your code must pass `make fmt` and `make lint` without errors.
- **TUI Guidelines:** When adding or modifying the UI in `internal/tui/`, rely on `lipgloss` for styling and keep components modular as standard `bubbletea.Model`s.
- **Game Logic:** New games should be implemented in their own subpackage under `internal/game/` and must implement the `game.Rules` interface. Remember to register new games in `cmd/server/main.go`.
- **Database Changes:** If you alter models in `internal/repository/`, ensure GORM auto-migrations are correctly handled or provide migration steps if necessary.

## Pull Request Process

1. **Keep it focused:** Try to make one PR per feature or bugfix. This makes it much easier to review.
2. **Describe your changes:** In the PR description, explain *what* you changed and *why*.
3. **Pass CI:** Ensure all GitHub Actions (tests, linters) pass. If you've run `make all` locally, you should be good to go.
4. **Review:** A maintainer will review your code. We may request changes or suggest alternative approaches. Don't be discouraged! It's part of the collaborative process.

## Adding a New Game

Adding a new card game is a great way to contribute!
1. Create a new directory under `internal/game/` (e.g., `internal/game/hearts`).
2. Implement the `game.Rules` interface for your game.
3. Hook up any specific TUI elements if your game requires custom views.
4. Register the game in `cmd/server/main.go` using `gameRegistry.Register(...)`.

Happy coding! 🃏
