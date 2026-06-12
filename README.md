# Terminal Card

Terminal Card is an SSH server that let you play multiplayer card games with your friends. Built with Go and [Charm](https://charm.sh/) ecosystem (Bubble Tea, Wish), it provides interactive TUI without requiring users to install any custom clients.

Currently features the classic game: **Crazy Eights**.

## Features

- **SSH-based Multiplayer:** No client installation needed; simply SSH into the server to play.
- **Rich TUI:** Powered by Charm's `bubbletea` and `lipgloss` for a modern terminal experience.
- **Persistent Stats:** Uses PostgreSQL and GORM to track users, matches, and ELO ratings.
- **Observability:** Built-in OpenTelemetry logging and metrics (Grafana, Loki, Alloy).

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details on how to get started, the development workflow, and our coding standards.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
