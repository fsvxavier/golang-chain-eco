// Package chathistory stores conversation history per session and wraps a
// runnable so it reads and appends history automatically
// (BaseChatMessageHistory / RunnableWithMessageHistory).
package chathistory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
)

// History is a mutable message log.
type History interface {
	Messages(ctx context.Context) ([]messages.Message, error)
	Add(ctx context.Context, ms ...messages.Message) error
	Clear(ctx context.Context) error
}

// Memory is an in-memory History (InMemoryChatMessageHistory).
type Memory struct {
	mu   sync.Mutex
	msgs []messages.Message
}

// Messages implements History.
func (m *Memory) Messages(context.Context) ([]messages.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]messages.Message(nil), m.msgs...), nil
}

// Add implements History.
func (m *Memory) Add(_ context.Context, ms ...messages.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, ms...)
	return nil
}

// Clear implements History.
func (m *Memory) Clear(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = nil
	return nil
}

// File persists history as JSON (FileChatMessageHistory).
type File struct {
	Path string
	mu   sync.Mutex
}

// Messages implements History.
func (f *File) Messages(context.Context) ([]messages.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}

func (f *File) read() ([]messages.Message, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ms []messages.Message
	return ms, json.Unmarshal(b, &ms)
}

// Add implements History.
func (f *File) Add(_ context.Context, ms ...messages.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, err := f.read()
	if err != nil {
		return err
	}
	b, err := json.Marshal(append(cur, ms...))
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, b, 0o644)
}

// Clear implements History.
func (f *File) Clear(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := os.Remove(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Store hands out a History per session ID.
type Store struct {
	mu       sync.Mutex
	sessions map[string]History
	New      func(sessionID string) History // default: &Memory{}
}

// Get returns (creating if needed) the history for id.
func (s *Store) Get(id string) History {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]History{}
	}
	h, ok := s.sessions[id]
	if !ok {
		if s.New != nil {
			h = s.New(id)
		} else {
			h = &Memory{}
		}
		s.sessions[id] = h
	}
	return h
}

// SessionKey is the configurable key holding the session ID.
const SessionKey = "session_id"

// WithHistory wraps r: before each call it prepends the session history to
// the input; afterwards it stores the new input and output messages. The
// session ID is read from runnables Config.Configurable["session_id"].
func WithHistory(r runnables.Runnable[[]messages.Message, messages.Message], get func(sessionID string) History) runnables.Runnable[[]messages.Message, messages.Message] {
	return runnables.Lambda("RunnableWithMessageHistory", func(ctx context.Context, in []messages.Message) (messages.Message, error) {
		id, _ := runnables.Configurable(ctx, SessionKey)
		sid, _ := id.(string)
		if sid == "" {
			return messages.Message{}, errors.New("chathistory: missing configurable session_id")
		}
		h := get(sid)
		past, err := h.Messages(ctx)
		if err != nil {
			return messages.Message{}, err
		}
		out, err := r.Invoke(ctx, append(past, in...))
		if err != nil {
			return out, err
		}
		return out, h.Add(ctx, append(append([]messages.Message(nil), in...), out)...)
	})
}
