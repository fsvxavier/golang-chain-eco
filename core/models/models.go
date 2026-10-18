// Package models defines the chat model abstraction (BaseChatModel) and the
// wrappers built on it: tool binding, structured output, caching, rate
// limiting, plus a scripted fake model for tests and offline examples.
package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// ResponseFormat requests provider-native JSON output conforming to Schema.
type ResponseFormat struct {
	Name   string
	Schema map[string]any
	Strict bool
}

// Options are per-call generation settings.
type Options struct {
	Model          string // override the model id
	Tools          []tools.Definition
	ToolChoice     string // "", "auto", "any"/"required", "none" or a tool name
	Temperature    *float64
	TopP           *float64
	MaxTokens      int
	Stop           []string
	ResponseFormat *ResponseFormat
	Extra          map[string]any // provider-specific request fields
}

// Option mutates Options.
type Option func(*Options)

// Apply builds Options from opts.
func Apply(opts ...Option) Options {
	var o Options
	for _, f := range opts {
		f(&o)
	}
	return o
}

// WithModel overrides the model id.
func WithModel(m string) Option { return func(o *Options) { o.Model = m } }

// WithTools exposes tools to the model.
func WithTools(ts ...tools.Tool) Option {
	return func(o *Options) { o.Tools = append(o.Tools, tools.DefineAll(ts)...) }
}

// WithToolDefinitions exposes raw tool definitions.
func WithToolDefinitions(ds ...tools.Definition) Option {
	return func(o *Options) { o.Tools = append(o.Tools, ds...) }
}

// WithToolChoice forces tool usage ("any", "none", "auto" or a tool name).
func WithToolChoice(c string) Option { return func(o *Options) { o.ToolChoice = c } }

// WithTemperature sets the sampling temperature.
func WithTemperature(t float64) Option { return func(o *Options) { o.Temperature = &t } }

// WithTopP sets nucleus sampling.
func WithTopP(p float64) Option { return func(o *Options) { o.TopP = &p } }

// WithMaxTokens caps output tokens.
func WithMaxTokens(n int) Option { return func(o *Options) { o.MaxTokens = n } }

// WithStop sets stop sequences.
func WithStop(s ...string) Option { return func(o *Options) { o.Stop = s } }

// WithResponseFormat requests JSON schema output.
func WithResponseFormat(rf ResponseFormat) Option { return func(o *Options) { o.ResponseFormat = &rf } }

// WithExtra sets a provider-specific request field.
func WithExtra(k string, v any) Option {
	return func(o *Options) {
		if o.Extra == nil {
			o.Extra = map[string]any{}
		}
		o.Extra[k] = v
	}
}

// ChatModel is implemented by every chat model provider.
type ChatModel interface {
	// Generate returns the complete AI message.
	Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error)
	// Stream yields AI message chunks; merge them with Message.Merge and
	// call Finalize on the result.
	Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error]
	// ModelID names the underlying model ("provider:model").
	ModelID() string
}

// Track wraps a generation with model callbacks. Providers call it so that
// tracers and the LangGraph "messages" stream mode observe every call.
func Track(ctx context.Context, name string, msgs []messages.Message, fn func(ctx context.Context) (messages.Message, error)) (messages.Message, error) {
	ctx, end := callbacks.Start(ctx, callbacks.ModelStart, name, msgs)
	out, err := fn(ctx)
	end(out, err)
	return out, err
}

// EmitToken reports a streamed chunk to callbacks (on_llm_new_token).
func EmitToken(ctx context.Context, chunk messages.Message) {
	callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.ModelNewToken, Chunk: chunk})
}

// Collect drains a chunk stream into a final message.
func Collect(seq iter.Seq2[messages.Message, error]) (messages.Message, error) {
	var acc messages.Message
	for c, err := range seq {
		if err != nil {
			return acc, err
		}
		acc = acc.Merge(c)
	}
	acc = acc.Finalize()
	if acc.Role == "" {
		acc.Role = messages.RoleAI
	}
	return acc, nil
}

// bound is a model with default options (bind / bind_tools).
type bound struct {
	ChatModel
	defaults []Option
}

// Bind returns m with default options applied before per-call options.
func Bind(m ChatModel, defaults ...Option) ChatModel {
	if b, ok := m.(*bound); ok {
		return &bound{b.ChatModel, append(append([]Option(nil), b.defaults...), defaults...)}
	}
	return &bound{m, defaults}
}

// BindTools binds tools (bind_tools).
func BindTools(m ChatModel, ts ...tools.Tool) ChatModel { return Bind(m, WithTools(ts...)) }

func (b *bound) Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error) {
	return b.ChatModel.Generate(ctx, msgs, append(append([]Option(nil), b.defaults...), opts...)...)
}

func (b *bound) Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error] {
	return b.ChatModel.Stream(ctx, msgs, append(append([]Option(nil), b.defaults...), opts...)...)
}

// Unwrap returns the model below any Bind.
func Unwrap(m ChatModel) ChatModel {
	for {
		b, ok := m.(*bound)
		if !ok {
			return m
		}
		m = b.ChatModel
	}
}

// AsRunnable exposes m as Runnable[[]Message, Message] with streaming.
func AsRunnable(m ChatModel, opts ...Option) runnables.Runnable[[]messages.Message, messages.Message] {
	return modelRunnable{m, opts}
}

type modelRunnable struct {
	m    ChatModel
	opts []Option
}

func (r modelRunnable) Invoke(ctx context.Context, in []messages.Message) (messages.Message, error) {
	return r.m.Generate(ctx, in, r.opts...)
}

func (r modelRunnable) Stream(ctx context.Context, in []messages.Message) iter.Seq2[messages.Message, error] {
	return r.m.Stream(ctx, in, r.opts...)
}

// StructuredMethod selects how structured output is obtained.
type StructuredMethod string

const (
	// MethodTool forces a tool call whose arguments are the output (default).
	MethodTool StructuredMethod = "function_calling"
	// MethodJSONSchema uses provider-native JSON schema output.
	MethodJSONSchema StructuredMethod = "json_schema"
)

// Structured returns a runnable producing T (with_structured_output).
func Structured[T any](m ChatModel, method StructuredMethod) runnables.Runnable[[]messages.Message, T] {
	schema := jsonschema.For[T]()
	name := schema.Title
	if name == "" {
		name = "output"
	}
	schema.Title = ""
	return runnables.Lambda("StructuredOutput", func(ctx context.Context, in []messages.Message) (T, error) {
		var out T
		if method == MethodJSONSchema {
			msg, err := m.Generate(ctx, in, WithResponseFormat(ResponseFormat{Name: name, Schema: schema.Map(), Strict: true}))
			if err != nil {
				return out, err
			}
			return out, json.Unmarshal([]byte(ExtractJSON(msg.Text())), &out)
		}
		def := tools.Definition{Name: name, Description: "Respond with this structured output", Parameters: schema.Map()}
		msg, err := m.Generate(ctx, in, WithToolDefinitions(def), WithToolChoice(name))
		if err != nil {
			return out, err
		}
		if len(msg.ToolCalls) == 0 {
			// Some models answer in plain JSON even when a tool is forced.
			if err := json.Unmarshal([]byte(ExtractJSON(msg.Text())), &out); err == nil {
				return out, nil
			}
			return out, fmt.Errorf("models: structured output: model returned no tool call")
		}
		return out, tools.Decode(msg.ToolCalls[0].Args, &out)
	})
}

// ExtractJSON returns the JSON object or array inside s, stripping markdown
// fences and surrounding prose.
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		}
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return s
	}
	end := strings.LastIndexAny(s, "}]")
	if end < start {
		return s[start:]
	}
	return s[start : end+1]
}

// ---------------------------------------------------------------- Fake

// Fake is a scripted model. It returns Responses in order (cycling when
// Cycle is set) or, when Func is set, whatever Func returns. Every call is
// recorded in Calls.
type Fake struct {
	Responses []messages.Message
	Func      func(msgs []messages.Message, o Options) (messages.Message, error)
	Cycle     bool
	Name      string

	mu    sync.Mutex
	i     int
	Calls [][]messages.Message
}

// NewFake returns a Fake answering with the given messages.
func NewFake(responses ...messages.Message) *Fake { return &Fake{Responses: responses} }

// ModelID implements ChatModel.
func (f *Fake) ModelID() string {
	if f.Name != "" {
		return f.Name
	}
	return "fake:fake-chat-model"
}

// Generate implements ChatModel.
func (f *Fake) Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error) {
	return Track(ctx, f.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		return f.next(msgs, Apply(opts...))
	})
}

func (f *Fake) next(msgs []messages.Message, o Options) (messages.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, append([]messages.Message(nil), msgs...))
	if f.Func != nil {
		m, err := f.Func(msgs, o)
		if m.Role == "" {
			m.Role = messages.RoleAI
		}
		if m.ID == "" {
			m.ID = messages.NewID()
		}
		for j := range m.ToolCalls {
			if m.ToolCalls[j].ID == "" {
				m.ToolCalls[j].ID = messages.NewID()
			}
		}
		return m, err
	}
	if len(f.Responses) == 0 {
		last, _ := messages.Last(msgs)
		return messages.AI("echo: " + last.Text()), nil
	}
	if f.i >= len(f.Responses) {
		if !f.Cycle {
			return messages.Message{}, errors.New("models: fake model has no more responses")
		}
		f.i = 0
	}
	m := f.Responses[f.i].Clone()
	f.i++
	if m.Role == "" {
		m.Role = messages.RoleAI
	}
	if m.ID == "" {
		m.ID = messages.NewID()
	}
	for j := range m.ToolCalls {
		if m.ToolCalls[j].ID == "" {
			m.ToolCalls[j].ID = fmt.Sprintf("call_%d_%d", f.i, j)
		}
	}
	return m, nil
}

// Stream implements ChatModel by splitting the content into word chunks.
func (f *Fake) Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		_, _ = Track(ctx, f.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			m, err := f.next(msgs, Apply(opts...))
			if err != nil {
				yield(messages.Message{}, err)
				return m, err
			}
			words := strings.SplitAfter(m.Content, " ")
			for i, w := range words {
				c := messages.Message{Role: messages.RoleAI, ID: m.ID, Content: w}
				if i == len(words)-1 {
					c.ToolCalls = m.ToolCalls
					c.Usage = m.Usage
				}
				EmitToken(ctx, c)
				if !yield(c, nil) {
					return m, nil
				}
			}
			return m, nil
		})
	}
}

// ---------------------------------------------------------------- cache

// Cache stores generations by prompt (BaseCache).
type Cache interface {
	Lookup(key string) (messages.Message, bool)
	Update(key string, m messages.Message)
}

// MemoryCache is an unbounded in-process Cache (InMemoryCache).
type MemoryCache struct{ m sync.Map }

// Lookup implements Cache.
func (c *MemoryCache) Lookup(k string) (messages.Message, bool) {
	v, ok := c.m.Load(k)
	if !ok {
		return messages.Message{}, false
	}
	return v.(messages.Message), true
}

// Update implements Cache.
func (c *MemoryCache) Update(k string, m messages.Message) { c.m.Store(k, m) }

type cached struct {
	ChatModel
	c Cache
}

// WithCache returns m answering repeated identical prompts from c.
func WithCache(m ChatModel, c Cache) ChatModel { return &cached{m, c} }

func cacheKey(id string, msgs []messages.Message, o Options) string {
	b, _ := json.Marshal(struct {
		ID   string
		Msgs []messages.Message
		O    Options
	}{id, msgs, o})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (c *cached) Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error) {
	k := cacheKey(c.ModelID(), msgs, Apply(opts...))
	if m, ok := c.c.Lookup(k); ok {
		return m, nil
	}
	m, err := c.ChatModel.Generate(ctx, msgs, opts...)
	if err == nil {
		c.c.Update(k, m)
	}
	return m, err
}

// ---------------------------------------------------------------- rate limit

// RateLimiter is a token bucket limiter (InMemoryRateLimiter).
type RateLimiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64
	tokens float64
	last   time.Time
}

// NewRateLimiter allows requestsPerSecond with the given burst size.
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	return &RateLimiter{rate: requestsPerSecond, burst: float64(max(burst, 1)), tokens: float64(max(burst, 1)), last: time.Now()}
}

// Wait blocks until a request may proceed.
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := time.Now()
		r.tokens = min(r.burst, r.tokens+now.Sub(r.last).Seconds()*r.rate)
		r.last = now
		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - r.tokens) / r.rate * float64(time.Second))
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

type limited struct {
	ChatModel
	rl *RateLimiter
}

// WithRateLimit throttles calls to m.
func WithRateLimit(m ChatModel, rl *RateLimiter) ChatModel { return &limited{m, rl} }

func (l *limited) Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error) {
	if err := l.rl.Wait(ctx); err != nil {
		return messages.Message{}, err
	}
	return l.ChatModel.Generate(ctx, msgs, opts...)
}

func (l *limited) Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		if err := l.rl.Wait(ctx); err != nil {
			yield(messages.Message{}, err)
			return
		}
		for c, err := range l.ChatModel.Stream(ctx, msgs, opts...) {
			if !yield(c, err) {
				return
			}
		}
	}
}
