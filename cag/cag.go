// Package cag answers questions by putting a whole document corpus in the
// system prompt, marked so providers can cache that prefix between calls.
package cag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// ErrCorpusTooLarge is returned when the corpus does not fit the window and no Fallback is set.
var ErrCorpusTooLarge = errors.New("cag: corpus does not fit the context window")

// ErrEmptyCorpus is returned when there is no corpus text to put in the prompt.
var ErrEmptyCorpus = errors.New("cag: corpus is empty")

// PrefixCacher marks the system message so the provider may cache it.
type PrefixCacher interface {
	Mark(system messages.Message) messages.Message
}

// AnthropicCache sends the system text as one block with cache_control ephemeral.
type AnthropicCache struct{}

// Mark implements PrefixCacher.
func (AnthropicCache) Mark(system messages.Message) messages.Message {
	system.Parts = []messages.ContentPart{{
		Type:  "text",
		Text:  system.Content,
		Extra: map[string]any{"cache_control": map[string]any{"type": "ephemeral"}},
	}}
	system.Content = ""
	return system
}

// StablePrefix leaves the message alone; providers with automatic prefix caching
// only need the prefix to be byte-stable.
type StablePrefix struct{}

// Mark implements PrefixCacher.
func (StablePrefix) Mark(system messages.Message) messages.Message { return system }

// CacherFor picks AnthropicCache for "anthropic:" models and StablePrefix otherwise.
func CacherFor(modelID string) PrefixCacher {
	if strings.HasPrefix(modelID, "anthropic:") {
		return AnthropicCache{}
	}
	return StablePrefix{}
}

// Engine answers from a cached corpus prefix, falling back when the corpus does not fit.
type Engine struct {
	Model    models.ChatModel
	Corpus   *Corpus
	Window   int // default 200000 tokens
	Reserve  int // tokens kept free for history, question and answer; default 4096
	Counter  knowledge.TokenCounter
	Cacher   PrefixCacher // default CacherFor(Model.ModelID())
	Fallback knowledge.Answerer
	System   string // default knowledge.DefaultSystem

	mu sync.RWMutex
}

// Answer implements knowledge.Answerer.
func (e *Engine) Answer(ctx context.Context, q knowledge.Question) (knowledge.Answer, error) {
	if strings.TrimSpace(q.Text) == "" {
		return knowledge.Answer{}, knowledge.ErrEmptyQuestion
	}
	if e.Model == nil {
		return knowledge.Answer{}, errors.New("cag: no model")
	}
	e.mu.RLock()
	corpus := e.Corpus
	e.mu.RUnlock()
	if corpus.Text() == "" {
		return knowledge.Answer{}, ErrEmptyCorpus
	}

	window, reserve := e.Window, e.Reserve
	if window <= 0 {
		window = 200000
	}
	if reserve <= 0 {
		reserve = 4096
	}
	tokens := corpus.Tokens(e.Counter)
	if tokens+reserve > window {
		reason := fmt.Sprintf("corpus %d tokens + reserve %d exceeds window %d", tokens, reserve, window)
		if e.Fallback == nil {
			return knowledge.Answer{}, fmt.Errorf("%w: %s", ErrCorpusTooLarge, reason)
		}
		ans, err := e.Fallback.Answer(ctx, q)
		if err != nil {
			return ans, fmt.Errorf("cag: fallback: %w", err)
		}
		ans.Trace = append([]knowledge.Step{{Name: "fallback", Detail: reason}}, ans.Trace...)
		return ans, nil
	}

	system := e.System
	if system == "" {
		system = knowledge.DefaultSystem
	}
	cacher := e.Cacher
	if cacher == nil {
		cacher = CacherFor(e.Model.ModelID())
	}
	sys := cacher.Mark(messages.System(system + "\n\nContext:\n" + corpus.Text()))

	msgs := make([]messages.Message, 0, len(q.History)+2)
	msgs = append(msgs, sys)
	msgs = append(msgs, q.History...)
	msgs = append(msgs, messages.Human(q.Text))

	resp, err := e.Model.Generate(ctx, msgs)
	if err != nil {
		return knowledge.Answer{}, fmt.Errorf("cag: generate: %w", err)
	}
	ans := knowledge.Answer{
		Text:     resp.Text(),
		Sources:  knowledge.SourcesFromDocs(corpus.Documents()),
		Strategy: "cag",
		Usage:    resp.Usage,
		Trace:    []knowledge.Step{{Name: "corpus_hash", Detail: corpus.Hash()}},
	}
	if resp.Usage != nil && resp.Usage.CacheReadTokens > 0 {
		ans.Trace = append(ans.Trace, knowledge.Step{Name: "cache_hit",
			Detail: fmt.Sprintf("cache_read_tokens=%d", resp.Usage.CacheReadTokens)})
	}
	return ans, nil
}

// SetCorpus swaps the corpus and reports whether its hash changed.
func (e *Engine) SetCorpus(c *Corpus) (changed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	changed = e.Corpus.Hash() != c.Hash()
	e.Corpus = c
	return changed
}
