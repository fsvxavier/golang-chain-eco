package ciam

import (
	"context"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

var memNS = []string{"ciam", "memories"}

// Memory is a note kept across sessions and projects.
type Memory struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"` // decision, note, bug, ...
	Content string    `json:"content"`
	Score   float64   `json:"score"`
	At      time.Time `json:"created_at"`
}

func (m *Manager) memories() *langmem.IndexedStore {
	return langmem.NewIndexedStore(m.Store, m.Embedder)
}

// Remember stores a decision or note, searchable by meaning (it is a langmem
// memory under the ["ciam","memories"] namespace, so a langmem Manager or the
// manage_memory tool can also curate it).
func (m *Manager) Remember(ctx context.Context, memType, content string) (string, error) {
	if memType == "" {
		memType = "note"
	}
	id := messages.NewID()
	err := m.memories().Put(ctx, memNS, id, map[string]any{"kind": memType, "content": content, "created_at": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return "", err
	}
	return id, m.persist()
}

// Recall returns the memories most relevant to query.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]Memory, error) {
	hits, err := langmem.Query(ctx, m.memories(), memNS, query, nil, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Memory, 0, len(hits))
	for _, h := range hits {
		mem := Memory{ID: h.Key, Score: h.Score}
		mem.Type, _ = h.Value["kind"].(string)
		mem.Content, _ = h.Value["content"].(string)
		if s, _ := h.Value["created_at"].(string); s != "" {
			mem.At, _ = time.Parse(time.RFC3339, s)
		}
		out = append(out, mem)
	}
	return out, nil
}

// Forgets removes a memory.
func (m *Manager) ForgetMemory(ctx context.Context, id string) error {
	if err := m.Store.Delete(ctx, memNS, id); err != nil {
		return err
	}
	return m.persist()
}
