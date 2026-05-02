package game

import "sync"

type Registry struct {
	mu    sync.RWMutex
	games map[string]func() Rules
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(name string, factory func() Rules) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[name] = factory
}

func (r *Registry) GameNames() []string {
	gameNames := make([]string, len(r.games))
	for game := range r.games {
		gameNames = append(gameNames, game)
	}
	return gameNames
}
