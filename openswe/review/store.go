package review

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// Store keeps the findings of every pull request, plus per-PR review state.
// One PR has one evolving findings list: later pushes and human replies
// re-enter it instead of starting independent reviews. It works over any
// store.Store (memory, SQL, Redis, Mongo...).
type Store struct {
	S  store.Store
	mu sync.Mutex
}

// NewStore wraps st.
func NewStore(st store.Store) *Store { return &Store{S: st} }

func findingsNS(k github.PRKey) []string {
	return []string{"openswe", "findings", k.Owner, k.Repo, strconv.Itoa(k.Number)}
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func fromMap(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Add records a finding. A finding with the same fingerprint as an existing
// open one is not duplicated: the existing one is returned with created=false.
func (s *Store) Add(ctx context.Context, k github.PRKey, f Finding) (Finding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.list(ctx, k)
	if err != nil {
		return Finding{}, false, err
	}
	for _, e := range all {
		if e.Fingerprint == f.Fingerprint && e.Status == Open {
			return e, false, nil
		}
	}
	if f.ID == "" {
		f.ID = messages.NewID()[:12]
	}
	if f.Status == "" {
		f.Status = Open
	}
	if f.Surface == "" {
		f.Surface = NotSurfaced
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	m, err := toMap(f)
	if err != nil {
		return Finding{}, false, err
	}
	return f, true, s.S.Put(ctx, findingsNS(k), f.ID, m)
}

func (s *Store) list(ctx context.Context, k github.PRKey) ([]Finding, error) {
	items, err := s.S.Search(ctx, findingsNS(k), nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Finding, 0, len(items))
	for _, it := range items {
		var f Finding
		if err := fromMap(it.Value, &f); err != nil {
			return nil, fmt.Errorf("review: corrupt finding %s: %w", it.Key, err)
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Finding) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return cmpString(a.ID, b.ID)
	})
	return out, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// List returns every finding of a PR, oldest first.
func (s *Store) List(ctx context.Context, k github.PRKey) ([]Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(ctx, k)
}

// Get returns one finding.
func (s *Store) Get(ctx context.Context, k github.PRKey, id string) (Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(ctx, k, id)
}

func (s *Store) get(ctx context.Context, k github.PRKey, id string) (Finding, error) {
	it, err := s.S.Get(ctx, findingsNS(k), id)
	if err != nil {
		return Finding{}, err
	}
	if it == nil {
		return Finding{}, fmt.Errorf("review: finding %q not found", id)
	}
	var f Finding
	return f, fromMap(it.Value, &f)
}

// Update applies fn to a finding atomically (with respect to this Store).
func (s *Store) Update(ctx context.Context, k github.PRKey, id string, fn func(*Finding) error) (Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.get(ctx, k, id)
	if err != nil {
		return f, err
	}
	surface := f.Surface
	if err := fn(&f); err != nil {
		return f, err
	}
	if surfaceRank(f.Surface) < surfaceRank(surface) { // surface only moves forward
		f.Surface = surface
	}
	m, err := toMap(f)
	if err != nil {
		return f, err
	}
	return f, s.S.Put(ctx, findingsNS(k), id, m)
}

func surfaceRank(s Surface) int {
	switch s {
	case NotSurfaced:
		return 0
	case Surfaced:
		return 1
	case ResolvePending:
		return 2
	case SurfaceDone:
		return 3
	}
	return 0
}

// PRState is the per-PR review bookkeeping.
type PRState struct {
	// LastReviewedSHA is the head the last published review covered.
	LastReviewedSHA string `json:"last_reviewed_sha,omitempty"`
	// Watch makes later pushes trigger re-reviews.
	Watch   bool           `json:"watch"`
	Reviews []ReviewRecord `json:"reviews,omitempty"`
}

// ReviewRecord is one published review.
type ReviewRecord struct {
	ReviewID int64     `json:"review_id"`
	HeadSHA  string    `json:"head_sha"`
	Event    string    `json:"event"`
	At       time.Time `json:"at"`
}

func stateNS(k github.PRKey) []string { return []string{"openswe", "reviewstate", k.Owner, k.Repo} }

// State loads a PR's review state (zero value when none).
func (s *Store) State(ctx context.Context, k github.PRKey) (PRState, error) {
	it, err := s.S.Get(ctx, stateNS(k), strconv.Itoa(k.Number))
	if err != nil || it == nil {
		return PRState{}, err
	}
	var st PRState
	return st, fromMap(it.Value, &st)
}

// SetState saves a PR's review state.
func (s *Store) SetState(ctx context.Context, k github.PRKey, st PRState) error {
	m, err := toMap(st)
	if err != nil {
		return err
	}
	return s.S.Put(ctx, stateNS(k), strconv.Itoa(k.Number), m)
}

// UpdateState read-modify-writes the state under the store lock.
func (s *Store) UpdateState(ctx context.Context, k github.PRKey, fn func(*PRState)) (PRState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.State(ctx, k)
	if err != nil {
		return st, err
	}
	fn(&st)
	return st, s.SetState(ctx, k, st)
}
