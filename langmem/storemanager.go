package langmem

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// DefaultKey is the store key of the seeded default memory (profiles).
const DefaultKey = "default"

// Seed is the initial memory written when the namespace is empty. With
// WithInserts(false) it implements a profile: a single document that is only
// refined, never multiplied.
type Seed struct {
	// Kind is a schema name (default: the first schema).
	Kind string
	// Content is a struct/map matching the schema, or a string for the
	// default Memory schema.
	Content any
}

// Phase is an extra consolidation pass run after the main extraction, over
// the memories only (MemoryPhase).
type Phase struct {
	// Instructions default to a deduplicate/consolidate/enrich prompt.
	Instructions    string
	IncludeMessages bool
	NoInserts       bool
	NoDeletes       bool
}

// WithStore fixes the store; otherwise the one attached to the running graph
// (graph.WithStore) is used.
func WithStore(s store.Store) Option { return func(c *config) { c.store = s } }

// WithQueryModel lets a model write the retrieval queries (as parallel
// search_memory calls) instead of searching with windows of the conversation.
func WithQueryModel(m models.ChatModel) Option { return func(c *config) { c.queryModel = m } }

// WithQueryLimit caps how many existing memories are shown to the extractor
// (default 5).
func WithQueryLimit(n int) Option { return func(c *config) { c.queryLimit = n } }

// WithDefault seeds the namespace with seed when it is empty.
func WithDefault(seed Seed) Option { return func(c *config) { c.defaultSeed = &seed } }

// WithDefaultFactory is WithDefault computed from the run (e.g. the user's
// name from Configurable).
func WithDefaultFactory(f func(context.Context) (Seed, error)) Option {
	return func(c *config) { c.defaultFactory = f }
}

// WithPhases adds consolidation passes.
func WithPhases(p ...Phase) Option { return func(c *config) { c.phases = p } }

// Payload is the input of StoreManager.Invoke and of ReflectionExecutor.
type Payload struct {
	Messages []messages.Message
	// MaxSteps of refinement (default 1).
	MaxSteps int
}

// Put is a write the manager applied.
type Put struct {
	Namespace []string
	Key       string
	Value     map[string]any
}

// StoreManager is a Manager wired to a store (create_memory_store_manager):
// it fetches the memories relevant to the conversation, lets the model change
// them, runs the optional phases, and writes back only what changed.
//
// Items are stored as {"kind": <schema name>, "content": <content>}.
type StoreManager struct {
	*Manager
	Namespace Namespace
}

// NewStoreManager builds a store manager. namespace may hold {variables}; nil
// means DefaultNamespace. Defaults: inserts, updates and deletes on, 5
// memories retrieved.
func NewStoreManager(model models.ChatModel, namespace Namespace, opts ...Option) *StoreManager {
	if len(namespace) == 0 {
		namespace = DefaultNamespace
	}
	c := newConfig(true, opts)
	if c.queryLimit <= 0 {
		c.queryLimit = 5
	}
	return &StoreManager{Manager: &Manager{Model: model, cfg: c}, Namespace: namespace}
}

func (m *StoreManager) resolve(ctx context.Context) (store.Store, []string, error) {
	st := m.cfg.store
	if st == nil {
		st = graph.StoreFrom(ctx)
	}
	if st == nil {
		return nil, nil, fmt.Errorf("%w: no store: use WithStore or compile the graph WithStore", ErrConfiguration)
	}
	ns, err := m.Namespace.Resolve(ctx)
	return st, ns, err
}

// Process extracts memories from msgs (one step) and applies the changes.
func (m *StoreManager) Process(ctx context.Context, msgs []messages.Message) ([]Put, error) {
	return m.Invoke(ctx, Payload{Messages: msgs})
}

// Invoke runs a full memory-formation pass and returns the writes it made.
func (m *StoreManager) Invoke(ctx context.Context, in Payload) ([]Put, error) {
	st, ns, err := m.resolve(ctx)
	if err != nil {
		return nil, err
	}
	hits, err := m.retrieve(ctx, st, ns, in.Messages)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 && (m.cfg.defaultSeed != nil || m.cfg.defaultFactory != nil) {
		seed, err := m.seed(ctx)
		if err != nil {
			return nil, err
		}
		val := map[string]any{"kind": seed.Kind, "content": seed.Content}
		if err := st.Put(ctx, ns, DefaultKey, val); err != nil {
			return nil, err
		}
		it, err := st.Get(ctx, ns, DefaultKey)
		if err != nil || it == nil {
			return nil, fmt.Errorf("langmem: seed not readable after put: %v", err)
		}
		hits = []Hit{{Item: *it}}
	}

	original := map[string]store.Item{}
	var current []Extracted
	for _, h := range hits {
		original[h.Key] = h.Item
		current = append(current, fromItem(h.Item))
	}
	removed := map[string]bool{}
	merge := func(out []Extracted) {
		idx := map[string]int{}
		for i, e := range current {
			idx[e.ID] = i
		}
		for _, e := range out {
			switch i, ok := idx[e.ID]; {
			case e.Deleted:
				if ok {
					current = slices.Delete(current, i, i+1)
					idx = map[string]int{}
					for j, c := range current {
						idx[c.ID] = j
					}
				}
				if _, inStore := original[e.ID]; inStore {
					removed[e.ID] = true
				}
			case ok:
				current[i] = e
			default:
				current = append(current, e)
				idx[e.ID] = len(current) - 1
			}
		}
	}

	out, err := m.Extract(ctx, in.Messages, current, in.MaxSteps)
	if err != nil {
		return nil, err
	}
	merge(out)
	for _, ph := range m.cfg.phases {
		pm := &Manager{Model: m.Model, cfg: m.cfg}
		pm.cfg.instructions = ph.Instructions
		if pm.cfg.instructions == "" {
			pm.cfg.instructions = "You are a memory manager. Deduplicate, consolidate, and enrich these memories."
		}
		pm.cfg.inserts, pm.cfg.deletes = !ph.NoInserts, !ph.NoDeletes
		var pmsgs []messages.Message
		if ph.IncludeMessages {
			pmsgs = in.Messages
		}
		out, err := pm.Extract(ctx, pmsgs, current, 1)
		if err != nil {
			return nil, err
		}
		merge(out)
	}

	var puts []Put
	for _, e := range current {
		val := map[string]any{"kind": e.Kind, "content": e.Content}
		if old, ok := original[e.ID]; ok {
			if sameValue(old.Value, val) {
				continue
			}
			puts = append(puts, Put{old.Namespace, old.Key, val})
		} else {
			puts = append(puts, Put{ns, e.ID, val})
		}
	}
	for _, p := range puts {
		if err := st.Put(ctx, p.Namespace, p.Key, p.Value); err != nil {
			return puts, err
		}
	}
	for id := range removed {
		if err := st.Delete(ctx, original[id].Namespace, id); err != nil {
			return puts, err
		}
	}
	return puts, nil
}

func (m *StoreManager) seed(ctx context.Context) (Seed, error) {
	var s Seed
	if m.cfg.defaultFactory != nil {
		var err error
		if s, err = m.cfg.defaultFactory(ctx); err != nil {
			return s, err
		}
	} else {
		s = *m.cfg.defaultSeed
	}
	if s.Kind == "" {
		s.Kind = m.cfg.schemas[0].Name
	}
	if str, ok := s.Content.(string); ok {
		s.Content = map[string]any{"content": str}
	}
	// normalise typed structs to JSON maps
	b, err := json.Marshal(s.Content)
	if err != nil {
		return s, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return s, err
	}
	s.Content = v
	return s, nil
}

func sameValue(a, b map[string]any) bool {
	x, _ := json.Marshal(map[string]any{"kind": a["kind"], "content": normalize(a["content"])})
	y, _ := json.Marshal(map[string]any{"kind": b["kind"], "content": normalize(b["content"])})
	return string(x) == string(y)
}

func fromItem(it store.Item) Extracted {
	kind, _ := it.Value["kind"].(string)
	if kind == "" {
		kind = MemorySchema.Name
	}
	return Extracted{ID: it.Key, Kind: kind, Content: normalize(it.Value["content"])}
}

// dilatedWindows are the conversation tails of 1, 2, 4... messages: recent
// context first, broader context after (get_dialated_windows).
func dilatedWindows(msgs []messages.Message, n int) []string {
	var out []string
	seen := map[int]bool{}
	for i := 0; i < n && len(msgs) > 0; i++ {
		size := min(len(msgs), 1<<i)
		if seen[size] {
			break
		}
		seen[size] = true
		out = append(out, messages.Buffer(msgs[len(msgs)-size:]))
	}
	return out
}

func (m *StoreManager) retrieve(ctx context.Context, st store.Store, ns []string, msgs []messages.Message) ([]Hit, error) {
	limit := m.cfg.queryLimit
	type q struct {
		text   string
		filter map[string]any
	}
	var queries []q
	if m.cfg.queryModel != nil {
		def := searchDefinition("Queries should be formatted as hypothetical memories that would be relevant to the current conversation.", "search_memory")
		ai, err := m.cfg.queryModel.Generate(ctx, []messages.Message{messages.Human(
			"Use parallel tool calling to search for distinct memories relevant to this conversation:\n\n<convo>\n" + messages.Buffer(msgs) + "\n</convo>.")},
			models.WithToolDefinitions(def), models.WithToolChoice("any"))
		if err != nil {
			return nil, fmt.Errorf("langmem: query model: %w", err)
		}
		for _, tc := range ai.ToolCalls {
			text, _ := tc.Args["query"].(string)
			f, _ := tc.Args["filter"].(map[string]any)
			queries = append(queries, q{text, f})
		}
	} else {
		for _, w := range dilatedWindows(msgs, max(limit/4, 1)) {
			queries = append(queries, q{text: w})
		}
	}
	best := map[string]Hit{}
	for _, qu := range queries {
		hits, err := Query(ctx, st, ns, qu.text, qu.filter, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			if old, ok := best[h.Key]; !ok || h.Score > old.Score {
				best[h.Key] = h
			}
		}
	}
	// Top up with unranked memories until limit: a keyword-only store matches
	// few items, and an extractor that cannot see a memory duplicates it.
	if len(best) < limit {
		all, err := Query(ctx, st, ns, "", nil, 1<<30)
		if err != nil {
			return nil, err
		}
		for _, h := range all {
			if _, ok := best[h.Key]; !ok && len(best) < limit {
				best[h.Key] = h
			}
		}
	}
	out := make([]Hit, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b Hit) int { return strings.Compare(a.Key, b.Key) }) // deterministic ties
	return rank(out, limit), nil
}

// Search returns memories relevant to query (listing when empty).
func (m *StoreManager) Search(ctx context.Context, query string, filter map[string]any, limit int) ([]Hit, error) {
	st, ns, err := m.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return Query(ctx, st, ns, query, filter, limit)
}

// Get reads one memory by key.
func (m *StoreManager) Get(ctx context.Context, key string) (*store.Item, error) {
	st, ns, err := m.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, ns, key)
}

// Put writes a memory of the given kind under key.
func (m *StoreManager) Put(ctx context.Context, key, kind string, content any) error {
	st, ns, err := m.resolve(ctx)
	if err != nil {
		return err
	}
	v, err := jsonValue(normalize(content))
	if err != nil {
		return err
	}
	return st.Put(ctx, ns, key, map[string]any{"kind": kind, "content": v})
}

// Delete removes a memory by key.
func (m *StoreManager) Delete(ctx context.Context, key string) error {
	st, ns, err := m.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, ns, key)
}
