package game

import (
	"errors"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	games map[string]func() Rules
}

func NewRegistry() *Registry {
	return &Registry{
		games: make(map[string]func() Rules),
	}
}

func (r *Registry) Register(name string, factory func() Rules) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[name] = factory
}

func (r *Registry) GameNames() []string {
	gameNames := make([]string, 0, len(r.games))
	for game := range r.games {
		gameNames = append(gameNames, game)
	}
	return gameNames
}

func (r *Registry) Create(name string) (Rules, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.games[name]
	if !ok {
		return nil, errors.New("game not found in registry")
	}
	return factory(), nil
}
