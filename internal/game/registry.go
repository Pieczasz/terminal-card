package game

import (
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Module describes a playable card game plugin: display name, route slug, and rules factory.
type Module struct {
	Name    string
	Slug    string // e.g. "crazy_eights" → route "game_crazy_eights"
	Factory func() Rules
}

// RouteName returns the TUI route key for this module.
func (m Module) RouteName() string {
	return "game_" + m.Slug
}

type Registry struct {
	mu      sync.RWMutex
	games   map[string]func() Rules
	modules map[string]Module // keyed by display name
	order   []string          // registration order for GameNames
}

func NewRegistry() *Registry {
	return &Registry{
		games:   make(map[string]func() Rules),
		modules: make(map[string]Module),
	}
}

// RegisterModule registers a game module as the single source of truth for name, slug, and factory.
func (r *Registry) RegisterModule(m Module) {
	if m.Name == "" || m.Slug == "" || m.Factory == nil {
		panic("game.RegisterModule: Name, Slug, and Factory are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.games[m.Name]; !exists {
		r.order = append(r.order, m.Name)
	}
	r.games[m.Name] = m.Factory
	r.modules[m.Name] = m
}

// Register registers a game by display name. Prefer RegisterModule for new games.
func (r *Registry) Register(name string, factory func() Rules) {
	r.RegisterModule(Module{
		Name:    name,
		Slug:    slugify(name),
		Factory: factory,
	})
}

func slugify(name string) string {
	b := make([]byte, 0, len(name))
	prevUnderscore := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b = append(b, c+'a'-'A')
			prevUnderscore = false
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
			prevUnderscore = false
		case c == ' ' || c == '-' || c == '_':
			if !prevUnderscore && len(b) > 0 {
				b = append(b, '_')
				prevUnderscore = true
			}
		}
	}
	if len(b) > 0 && b[len(b)-1] == '_' {
		b = b[:len(b)-1]
	}
	return string(b)
}

func (r *Registry) GameNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.order)
}

func (r *Registry) Module(name string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[name]
	return m, ok
}

// RouteName returns the TUI route for a registered game display name.
func (r *Registry) RouteName(name string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[name]
	if !ok {
		return "", fmt.Errorf("game not found in registry: %s", name)
	}
	return m.RouteName(), nil
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
