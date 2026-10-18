// Package store provides the cross-thread long-term memory store of LangGraph
// (langgraph.store): hierarchical namespaces holding JSON-like items.
package store

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// Item is a stored value.
type Item struct {
	Namespace []string
	Key       string
	Value     map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the long-term memory interface.
type Store interface {
	Put(ctx context.Context, ns []string, key string, value map[string]any) error
	Get(ctx context.Context, ns []string, key string) (*Item, error)
	Delete(ctx context.Context, ns []string, key string) error
	// Search lists items whose namespace starts with prefix. Filter matches
	// top-level value fields by equality.
	Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]Item, error)
	ListNamespaces(ctx context.Context, prefix []string) ([][]string, error)
}

// Memory is an in-memory Store (InMemoryStore).
type Memory struct {
	mu    sync.RWMutex
	items map[string]Item
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{items: map[string]Item{}} }

func id(ns []string, key string) string { return strings.Join(ns, "\x00") + "\x01" + key }

func (m *Memory) Put(_ context.Context, ns []string, key string, v map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	it := Item{Namespace: slices.Clone(ns), Key: key, Value: maps.Clone(v), CreatedAt: now, UpdatedAt: now}
	if old, ok := m.items[id(ns, key)]; ok {
		it.CreatedAt = old.CreatedAt
	}
	m.items[id(ns, key)] = it
	return nil
}

func (m *Memory) Get(_ context.Context, ns []string, key string) (*Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	it, ok := m.items[id(ns, key)]
	if !ok {
		return nil, nil
	}
	it.Value = maps.Clone(it.Value)
	return &it, nil
}

func (m *Memory) Delete(_ context.Context, ns []string, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id(ns, key))
	return nil
}

func (m *Memory) Search(_ context.Context, prefix []string, filter map[string]any, limit int) ([]Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Item
	for _, it := range m.items {
		if len(it.Namespace) < len(prefix) || !slices.Equal(it.Namespace[:len(prefix)], prefix) {
			continue
		}
		match := true
		for k, v := range filter {
			if it.Value[k] != v {
				match = false
				break
			}
		}
		if match {
			it.Value = maps.Clone(it.Value)
			out = append(out, it)
		}
	}
	slices.SortFunc(out, func(a, b Item) int { return strings.Compare(id(a.Namespace, a.Key), id(b.Namespace, b.Key)) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) ListNamespaces(_ context.Context, prefix []string) ([][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string][]string{}
	for _, it := range m.items {
		if len(it.Namespace) >= len(prefix) && slices.Equal(it.Namespace[:len(prefix)], prefix) {
			seen[strings.Join(it.Namespace, "\x00")] = it.Namespace
		}
	}
	out := make([][]string, 0, len(seen))
	for _, ns := range seen {
		out = append(out, ns)
	}
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(strings.Join(a, "/"), strings.Join(b, "/")) })
	return out, nil
}
