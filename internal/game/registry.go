package game

import (
	"errors"
	"slices"
	"sync"
)

type Module struct {
	Name    string
	Slug    string // e.g. "crazy_eights"
	Factory func() Rules
}

type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module // keyed by display name
	order   []string          // registration order for GameNames
}

func NewRegistry() *Registry {
	return &Registry{modules: make(map[string]Module)}
}

func (r *Registry) RegisterModule(m Module) {
	if m.Name == "" || m.Slug == "" || m.Factory == nil {
		panic("game.RegisterModule: Name, Slug, and Factory are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.modules[m.Name]; !exists {
		r.order = append(r.order, m.Name)
	}
	r.modules[m.Name] = m
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

func (r *Registry) Create(name string) (Rules, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m, ok := r.modules[name]
	if !ok {
		return nil, errors.New("game not found in registry")
	}
	return m.Factory(), nil
}
