package ciam

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Manager is the entry point: it indexes projects into a store and answers
// searches. It is safe for concurrent use.
type Manager struct {
	// Embedder turns text into vectors (e.g. providers/ollama.Embeddings).
	Embedder embeddings.Embedder
	// Store persists chunks, file hashes and memories.
	Store store.Store
	// Model is optional; it enables Draft.
	Model models.ChatModel
	// Classifiers override the per-project-type defaults.
	Classifiers []Classifier
	// CacheSize and CacheTTL bound the search cache (defaults 256, 10 min).
	CacheSize int
	CacheTTL  time.Duration

	mu      sync.Mutex
	corpora map[string]*corpus
	cache   map[string]cacheEntry

	searches, cacheHits, tokensServed, tokensSaved atomic.Int64
}

type cacheEntry struct {
	at      time.Time
	results []Result
}

// New builds a manager. A nil store means an in-memory one.
func New(e embeddings.Embedder, st store.Store) *Manager {
	if st == nil {
		st = store.NewMemory()
	}
	return &Manager{Embedder: embeddings.NewCached(e), Store: st, corpora: map[string]*corpus{}, cache: map[string]cacheEntry{}}
}

func (m *Manager) persist() error {
	if s, ok := m.Store.(saver); ok {
		return s.Save()
	}
	return nil
}

// ProjectID is the identifier used for a project root (its directory name).
func ProjectID(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Base(abs)
}

// IndexOptions tunes Index.
type IndexOptions struct {
	// Project overrides the project id (default: the root directory name).
	Project string
	// Type forces the project type ("django", "go", "node", "python",
	// "generic"); empty auto-detects.
	Type string
	// Force re-embeds every file even when its hash is unchanged.
	Force bool
	// SkipDocs leaves docs/{adr,prd,plans,research} out of the index.
	SkipDocs bool
}

// IndexResult reports what an Index run did.
type IndexResult struct {
	Project   string `json:"project"`
	Type      string `json:"type"`
	Files     int    `json:"files"`
	Changed   int    `json:"changed"`
	Unchanged int    `json:"unchanged"`
	Removed   int    `json:"removed"`
	Chunks    int    `json:"chunks"`
	Tokens    int    `json:"tokens"`
}

const maxFileBytes = 1 << 20

var docDirs = map[string]string{"adr": "adr", "prd": "prd", "plans": "plan", "research": "research"}

func nsChunks(project string) []string { return []string{"ciam", "chunks", project} }
func nsFiles(project string) []string  { return []string{"ciam", "files", project} }

// Index walks root and updates the project index incrementally: files whose
// content hash did not change are skipped, changed ones are re-chunked and
// re-embedded, deleted ones are removed.
func (m *Manager) Index(ctx context.Context, root string, o IndexOptions) (IndexResult, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return IndexResult{}, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return IndexResult{}, fmt.Errorf("ciam: %s is not a directory", root)
	}
	project := o.Project
	if project == "" {
		project = ProjectID(root)
	}
	ptype := o.Type
	if ptype == "" {
		ptype = DetectType(root)
	}
	classifiers := m.Classifiers
	if len(classifiers) == 0 {
		classifiers = ClassifiersFor(ptype)
	}
	res := IndexResult{Project: project, Type: ptype}

	seen := map[string]bool{}
	var walkErr error
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		docType := ""
		if parts := strings.Split(filepath.ToSlash(rel), "/"); len(parts) >= 3 && parts[0] == "docs" {
			docType = docDirs[parts[1]]
		}
		if docType != "" && o.SkipDocs {
			return nil
		}
		if !IndexableExtensions[extOf(d.Name())] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		res.Files++
		content := string(data)
		hash := hashOf(content)
		if !o.Force {
			if it, _ := m.Store.Get(ctx, nsFiles(project), rel); it != nil && it.Value["hash"] == hash {
				res.Unchanged++
				res.Chunks += intOf(it.Value["chunks"])
				res.Tokens += intOf(it.Value["tokens"])
				return nil
			}
		}
		n, err := m.indexFile(ctx, project, rel, content, hash, docType, classifiers)
		if err != nil {
			walkErr = err
			return filepath.SkipAll
		}
		res.Changed++
		res.Chunks += n
		res.Tokens += EstimateTokens(content)
		return nil
	})
	if err == nil {
		err = walkErr
	}
	if err != nil {
		return res, err
	}
	// remove files that disappeared
	old, err := m.Store.Search(ctx, nsFiles(project), nil, 0)
	if err != nil {
		return res, err
	}
	for _, it := range old {
		if !seen[it.Key] {
			if err := m.dropFile(ctx, project, it.Key, intOf(it.Value["chunks"])); err != nil {
				return res, err
			}
			res.Removed++
		}
	}
	m.invalidate(project)
	return res, m.persist()
}

func (m *Manager) dropFile(ctx context.Context, project, file string, chunks int) error {
	for i := range chunks {
		if err := m.Store.Delete(ctx, nsChunks(project), chunkKey(file, i)); err != nil {
			return err
		}
	}
	return m.Store.Delete(ctx, nsFiles(project), file)
}

func chunkKey(file string, i int) string { return fmt.Sprintf("%s#%d", file, i) }

func (m *Manager) indexFile(ctx context.Context, project, rel, content, hash, docType string, cls []Classifier) (int, error) {
	kind := docType
	if kind == "" {
		for _, c := range cls {
			if kind = c(rel, content); kind != "" {
				break
			}
		}
	}
	if kind == "" {
		kind = "generic"
	}
	lines := chunkLines
	if docType != "" {
		lines = 80
	}
	var pieces []string
	for _, p := range SplitLines(content, lines, chunkOverlap) {
		pieces = append(pieces, Truncate(p))
	}
	// drop the previous version first so a shrinking file leaves no orphans
	if it, _ := m.Store.Get(ctx, nsFiles(project), rel); it != nil {
		if err := m.dropFile(ctx, project, rel, intOf(it.Value["chunks"])); err != nil {
			return 0, err
		}
	}
	for start := 0; start < len(pieces); start += 32 {
		batch := pieces[start:min(start+32, len(pieces))]
		vecs, err := m.Embedder.EmbedDocuments(ctx, batch)
		if err != nil {
			return 0, fmt.Errorf("ciam: embedding %s: %w", rel, err)
		}
		if len(vecs) != len(batch) {
			return 0, fmt.Errorf("ciam: embedder returned %d vectors for %d chunks", len(vecs), len(batch))
		}
		for j, text := range batch {
			i := start + j
			err := m.Store.Put(ctx, nsChunks(project), chunkKey(rel, i), map[string]any{
				"file": rel, "index": i, "type": kind, "content": text, "embedding": vecs[j]})
			if err != nil {
				return 0, err
			}
		}
	}
	err := m.Store.Put(ctx, nsFiles(project), rel, map[string]any{
		"hash": hash, "chunks": len(pieces), "type": kind, "tokens": EstimateTokens(content)})
	return len(pieces), err
}

// Forget removes a project from the index.
func (m *Manager) Forget(ctx context.Context, project string) error {
	files, err := m.Store.Search(ctx, nsFiles(project), nil, 0)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := m.dropFile(ctx, project, f.Key, intOf(f.Value["chunks"])); err != nil {
			return err
		}
	}
	m.invalidate(project)
	return m.persist()
}

// Watch re-indexes root every interval until ctx ends. Because Index is
// incremental, an idle tick costs one hash per file and no embedding calls.
func (m *Manager) Watch(ctx context.Context, root string, o IndexOptions, interval time.Duration, onResult func(IndexResult, error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		r, err := m.Index(ctx, root, o)
		if onResult != nil && (err != nil || r.Changed > 0 || r.Removed > 0) {
			onResult(r, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func toVec(v any) []float32 {
	switch x := v.(type) {
	case []float32:
		return x
	case []any:
		out := make([]float32, len(x))
		for i, e := range x {
			f, _ := e.(float64)
			out[i] = float32(f)
		}
		return out
	case []float64:
		out := make([]float32, len(x))
		for i, f := range x {
			out[i] = float32(f)
		}
		return out
	}
	return nil
}

// Projects lists the indexed projects.
func (m *Manager) Projects(ctx context.Context) ([]string, error) {
	nss, err := m.Store.ListNamespaces(ctx, []string{"ciam", "files"})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ns := range nss {
		if len(ns) >= 3 {
			out = append(out, ns[2])
		}
	}
	sort.Strings(out)
	return out, nil
}
