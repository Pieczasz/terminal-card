package lobby

import (
	"github.com/Pieczasz/terminal-card/internal/game"
)

// SessionAPI is the lobby surface a TUI session needs. *Manager implements it;
// tests and the router depend on this interface rather than the full Manager.
type SessionAPI interface {
	New(leader *game.Player, opts ...Option) (*Lobby, error)
	JoinLobbyByCode(code string, p *game.Player) error
	FindLobbyByCode(code string) (*Lobby, error)
	FindLobbyByPlayer(p *game.Player) *Lobby
	LeaveLobby(p *game.Player)
	Kick(host, target *game.Player) error
	BrowseLobbies(p *game.Player, f BrowseFilter) []BrowseEntry
	GameNames() []string
	ResumePlayer(p *game.Player) *Lobby
}

var _ SessionAPI = (*Manager)(nil)
