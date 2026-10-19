package ciam

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Result is a ranked chunk.
type Result struct {
	File    string  `json:"file"`
	Type    string  `json:"type"`
	Index   int     `json:"chunk"`
	Content string  `json:"content"`
	Score   float64 `json:"score"` // reciprocal-rank-fusion score
	Vector  float64 `json:"vector_score"`
	BM25    float64 `json:"bm25_score"`
}

// SearchRequest asks for the chunks most relevant to Query.
type SearchRequest struct {
	Project string
	Query   string
	// Types restricts results to chunk types (e.g. "model", "adr").
	Types []string
	// ExcludeTypes drops chunk types (e.g. the documents, to search code only).
	ExcludeTypes []string
	Limit        int // default 5
	// Compress strips comments and docstrings from the returned content.
	Compress bool
}

// corpus is the in-RAM view of one project, with BM25 statistics.
type corpus struct {
	chunks []Chunk
	tokens [][]string
	df     map[string]int
	avgLen float64
	// fileTokens maps file -> estimated tokens of the whole file, to estimate
	// how much context a search saved versus reading the files.
	fileTokens map[string]int
}

func (m *Manager) invalidate(project string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.corpora, project)
	m.cache = map[string]cacheEntry{}
}

func (m *Manager) corpusOf(ctx context.Context, project string) (*corpus, error) {
	m.mu.Lock()
	c := m.corpora[project]
	m.mu.Unlock()
	if c != nil {
		return c, nil
	}
	items, err := m.Store.Search(ctx, nsChunks(project), nil, 0)
	if err != nil {
		return nil, err
	}
	c = &corpus{df: map[string]int{}, fileTokens: map[string]int{}}
	files, _ := m.Store.Search(ctx, nsFiles(project), nil, 0)
	for _, f := range files {
		c.fileTokens[f.Key] = intOf(f.Value["tokens"])
	}
	var total int
	for _, it := range items {
		ch := Chunk{Project: project, Embedding: toVec(it.Value["embedding"])}
		ch.File, _ = it.Value["file"].(string)
		ch.Type, _ = it.Value["type"].(string)
		ch.Content, _ = it.Value["content"].(string)
		ch.Index = intOf(it.Value["index"])
		toks := Tokenize(ch.Content)
		seen := map[string]bool{}
		for _, t := range toks {
			if !seen[t] {
				seen[t] = true
				c.df[t]++
			}
		}
		total += len(toks)
		c.chunks = append(c.chunks, ch)
		c.tokens = append(c.tokens, toks)
	}
	if len(c.chunks) > 0 {
		c.avgLen = float64(total) / float64(len(c.chunks))
	}
	// deterministic order (store iteration order is not part of the contract)
	idx := make([]int, len(c.chunks))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		x, y := c.chunks[idx[a]], c.chunks[idx[b]]
		if x.File != y.File {
			return x.File < y.File
		}
		return x.Index < y.Index
	})
	chunks, toks := make([]Chunk, len(idx)), make([][]string, len(idx))
	for i, j := range idx {
		chunks[i], toks[i] = c.chunks[j], c.tokens[j]
	}
	c.chunks, c.tokens = chunks, toks
	m.mu.Lock()
	m.corpora[project] = c
	m.mu.Unlock()
	return c, nil
}

// Tokenize lowercases and splits on non-alphanumerics, additionally splitting
// snake_case and camelCase identifiers, so "LeadCapture" matches "lead".
func Tokenize(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		word := string(cur)
		out = append(out, strings.ToLower(word))
		// camelCase pieces
		var piece []rune
		pieces := 0
		emit := func() {
			if len(piece) > 0 {
				out = append(out, strings.ToLower(string(piece)))
				pieces++
				piece = piece[:0]
			}
		}
		for i, r := range cur {
			if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(cur[i-1]) || unicode.IsDigit(cur[i-1])) {
				emit()
			}
			piece = append(piece, r)
		}
		emit()
		if pieces == 1 { // single piece == the word itself, already added
			out = out[:len(out)-1]
		}
		cur = cur[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func (c *corpus) bm25(query []string, i int) float64 {
	const k1, b = 1.5, 0.75
	tf := map[string]int{}
	for _, t := range c.tokens[i] {
		tf[t]++
	}
	n := float64(len(c.chunks))
	dl := float64(len(c.tokens[i]))
	var score float64
	seen := map[string]bool{}
	for _, q := range query {
		if seen[q] || tf[q] == 0 {
			continue
		}
		seen[q] = true
		idf := math.Log(1 + (n-float64(c.df[q])+0.5)/(float64(c.df[q])+0.5))
		f := float64(tf[q])
		score += idf * f * (k1 + 1) / (f + k1*(1-b+b*dl/math.Max(c.avgLen, 1)))
	}
	return score
}

const rrfK = 60.0

// Search ranks the project's chunks for the query by fusing vector similarity
// with BM25 (Reciprocal Rank Fusion), so exact identifiers and paraphrases
// both find their chunk.
func (m *Manager) Search(ctx context.Context, req SearchRequest) ([]Result, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("ciam: empty query")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	key := hashOf(fmt.Sprint(req.Project, "|", req.Query, "|", req.Types, "|", req.ExcludeTypes, "|", limit, "|", req.Compress))
	ttl, size := m.CacheTTL, m.CacheSize
	if ttl == 0 {
		ttl = 10 * time.Minute
	}
	if size == 0 {
		size = 256
	}
	m.searches.Add(1)
	m.mu.Lock()
	if e, ok := m.cache[key]; ok && time.Since(e.at) < ttl {
		m.mu.Unlock()
		m.cacheHits.Add(1)
		return slices.Clone(e.results), nil
	}
	m.mu.Unlock()

	c, err := m.corpusOf(ctx, req.Project)
	if err != nil {
		return nil, err
	}
	var cand []int
	for i, ch := range c.chunks {
		if (len(req.Types) == 0 || slices.Contains(req.Types, ch.Type)) && !slices.Contains(req.ExcludeTypes, ch.Type) {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		return nil, nil
	}
	qvec, err := m.Embedder.EmbedQuery(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("ciam: embedding query: %w", err)
	}
	qtok := Tokenize(req.Query)
	vs, bs := make(map[int]float64, len(cand)), make(map[int]float64, len(cand))
	for _, i := range cand {
		vs[i] = cosine(qvec, c.chunks[i].Embedding)
		bs[i] = c.bm25(qtok, i)
	}
	rank := func(score map[int]float64, positive bool) map[int]int {
		order := slices.Clone(cand)
		sort.SliceStable(order, func(a, b int) bool { return score[order[a]] > score[order[b]] })
		r := map[int]int{}
		for pos, i := range order {
			if positive && score[i] <= 0 {
				break // a chunk with no keyword match gets no keyword rank
			}
			r[i] = pos + 1
		}
		return r
	}
	vr, br := rank(vs, false), rank(bs, true)
	out := make([]Result, 0, len(cand))
	for _, i := range cand {
		var rrf float64
		if r, ok := vr[i]; ok {
			rrf += 1 / (rrfK + float64(r))
		}
		if r, ok := br[i]; ok {
			rrf += 1 / (rrfK + float64(r))
		}
		ch := c.chunks[i]
		out = append(out, Result{File: ch.File, Type: ch.Type, Index: ch.Index, Content: ch.Content, Score: rrf, Vector: vs[i], BM25: bs[i]})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].Vector > out[b].Vector
	})
	if len(out) > limit {
		out = out[:limit]
	}

	files := map[string]bool{}
	var served int
	for i := range out {
		if req.Compress {
			out[i].Content = Compress(out[i].Content, extOf(out[i].File))
		}
		served += EstimateTokens(out[i].Content)
		files[out[i].File] = true
	}
	var wouldRead int
	for f := range files {
		wouldRead += c.fileTokens[f]
	}
	m.tokensServed.Add(int64(served))
	if wouldRead > served {
		m.tokensSaved.Add(int64(wouldRead - served))
	}

	m.mu.Lock()
	if len(m.cache) >= size {
		var oldest string
		var at time.Time
		for k, e := range m.cache {
			if oldest == "" || e.at.Before(at) {
				oldest, at = k, e.at
			}
		}
		delete(m.cache, oldest)
	}
	m.cache[key] = cacheEntry{at: time.Now(), results: slices.Clone(out)}
	m.mu.Unlock()
	return out, nil
}

// DecisionContext answers "what do I need to know before changing X?" in one
// call: relevant code plus the ADRs, PRDs and plans that explain why it is the
// way it is. Call it before implementing anything non-trivial.
func (m *Manager) DecisionContext(ctx context.Context, project, query string, limit int) (string, error) {
	if limit <= 0 {
		limit = 3
	}
	docTypes := []string{"adr", "prd", "plan", "research"}
	sections := []struct {
		title string
		types []string
	}{
		{"Code", nil},
		{"Architecture decisions (ADR)", []string{"adr"}},
		{"Requirements (PRD)", []string{"prd"}},
		{"Implementation plans", []string{"plan"}},
		{"Research", []string{"research"}},
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Decision context for: %q\n\n", query)
	for _, s := range sections {
		var exclude []string
		if s.types == nil {
			exclude = docTypes // the code section is code: the documents have their own sections
		}
		res, err := m.Search(ctx, SearchRequest{Project: project, Query: query, Types: s.types, ExcludeTypes: exclude, Limit: limit, Compress: s.types == nil})
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "## %s\n", s.title)
		if len(res) == 0 {
			if s.types != nil {
				b.WriteString("_none indexed_\n\n")
			} else {
				b.WriteString("_no results_\n\n")
			}
			continue
		}
		for _, r := range res {
			fmt.Fprintf(&b, "**%s** (`%s`, score %.3f)\n```\n%s\n```\n\n", r.File, r.Type, r.Score, r.Content)
		}
	}
	return b.String(), nil
}

// Status is a snapshot of what the manager indexed and saved.
type Status struct {
	Projects []ProjectStatus `json:"projects"`
	Memories int             `json:"memories"`
	// Searches counts Search calls; CacheHits those answered from the cache.
	Searches  int64 `json:"searches"`
	CacheHits int64 `json:"cache_hits"`
	// TokensServed is what searches returned; TokensSaved estimates what
	// reading the matched files whole would have cost beyond that.
	TokensServed int64 `json:"tokens_served"`
	TokensSaved  int64 `json:"tokens_saved_estimate"`
}

// ProjectStatus summarises one indexed project.
type ProjectStatus struct {
	Project string         `json:"project"`
	Files   int            `json:"files"`
	Chunks  int            `json:"chunks"`
	Tokens  int            `json:"tokens"`
	Types   map[string]int `json:"chunk_types"`
}

// Status reports index and usage statistics.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	st := Status{Searches: m.searches.Load(), CacheHits: m.cacheHits.Load(), TokensServed: m.tokensServed.Load(), TokensSaved: m.tokensSaved.Load()}
	projects, err := m.Projects(ctx)
	if err != nil {
		return st, err
	}
	for _, p := range projects {
		files, err := m.Store.Search(ctx, nsFiles(p), nil, 0)
		if err != nil {
			return st, err
		}
		ps := ProjectStatus{Project: p, Files: len(files), Types: map[string]int{}}
		for _, f := range files {
			n := intOf(f.Value["chunks"])
			ps.Chunks += n
			ps.Tokens += intOf(f.Value["tokens"])
			k, _ := f.Value["type"].(string)
			ps.Types[k] += n
		}
		st.Projects = append(st.Projects, ps)
	}
	mem, err := m.Store.Search(ctx, memNS, nil, 0)
	st.Memories = len(mem)
	return st, err
}
