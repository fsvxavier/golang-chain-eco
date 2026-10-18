package backends

import (
	"context"
	"fmt"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// FilesFromState reads the "files" state key.
func FilesFromState(st graph.State) map[string]FileData {
	m, _ := st["files"].(map[string]FileData)
	if m == nil {
		return map[string]FileData{}
	}
	return m
}

// FilesChannel is the channel spec of the "files" state key: updates are
// FilesUpdate values merged into the map (nil entries delete).
func FilesChannel() channels.Spec {
	return channels.BinOp(func(cur, upd any) (any, error) {
		out := map[string]FileData{}
		if c, ok := cur.(map[string]FileData); ok {
			for k, v := range c {
				out[k] = v
			}
		}
		u, ok := upd.(FilesUpdate)
		if !ok {
			return nil, fmt.Errorf("files: bad update type %T", upd)
		}
		for k, v := range u {
			if v == nil {
				delete(out, k)
			} else {
				out[k] = *v
			}
		}
		return out, nil
	}, func() any { return map[string]FileData{} })
}

// State keeps files in the graph state ("files" key). It is stateless
// itself: reads use the state of the running tool, writes are returned as a
// FilesUpdate for the tool to commit.
type State struct{}

func (State) files(ctx context.Context) map[string]FileData {
	return FilesFromState(prebuilt.StateOf(ctx))
}

func (s State) Ls(ctx context.Context, dir string) ([]FileInfo, error) {
	return lsFromFiles(s.files(ctx), dir), nil
}

func (s State) Read(ctx context.Context, p string) (string, error) {
	f, ok := s.files(ctx)[Clean(p)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	return f.Content, nil
}

func (s State) Write(ctx context.Context, p, content string) (FilesUpdate, error) {
	p = Clean(p)
	if _, ok := s.files(ctx)[p]; ok {
		return nil, fmt.Errorf("cannot write to %s because it already exists; read it and make an edit, or write to a new path", p)
	}
	now := time.Now()
	return FilesUpdate{p: {Content: content, CreatedAt: now, ModifiedAt: now}}, nil
}

func (s State) Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error) {
	p = Clean(p)
	f, ok := s.files(ctx)[p]
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	c, n, err := ApplyEdit(f.Content, old, new, all)
	if err != nil {
		return nil, 0, err
	}
	f.Content, f.ModifiedAt = c, time.Now()
	return FilesUpdate{p: &f}, n, nil
}

func (s State) Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error) {
	return globFiles(s.files(ctx), pattern, dir)
}

func (s State) Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	return grepFiles(s.files(ctx), pattern, dir, glob)
}

// Store keeps files in a LangGraph store so they persist across threads.
type Store struct {
	Store store.Store
	// Namespace prefix, e.g. []string{"filesystem", assistantID}.
	Namespace []string
}

func (s Store) load(ctx context.Context) (map[string]FileData, error) {
	items, err := s.Store.Search(ctx, s.Namespace, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]FileData{}
	for _, it := range items {
		c, _ := it.Value["content"].(string)
		out[it.Key] = FileData{Content: c, CreatedAt: it.CreatedAt, ModifiedAt: it.UpdatedAt}
	}
	return out, nil
}

func (s Store) Ls(ctx context.Context, dir string) ([]FileInfo, error) {
	f, err := s.load(ctx)
	return lsFromFiles(f, dir), err
}

func (s Store) Read(ctx context.Context, p string) (string, error) {
	it, err := s.Store.Get(ctx, s.Namespace, Clean(p))
	if err != nil {
		return "", err
	}
	if it == nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	c, _ := it.Value["content"].(string)
	return c, nil
}

func (s Store) Write(ctx context.Context, p, content string) (FilesUpdate, error) {
	p = Clean(p)
	if it, err := s.Store.Get(ctx, s.Namespace, p); err != nil {
		return nil, err
	} else if it != nil {
		return nil, fmt.Errorf("cannot write to %s because it already exists; read it and make an edit, or write to a new path", p)
	}
	return nil, s.Store.Put(ctx, s.Namespace, p, map[string]any{"content": content})
}

func (s Store) Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error) {
	c, err := s.Read(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	c, n, err := ApplyEdit(c, old, new, all)
	if err != nil {
		return nil, 0, err
	}
	return nil, n, s.Store.Put(ctx, s.Namespace, Clean(p), map[string]any{"content": c})
}

func (s Store) Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error) {
	f, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return globFiles(f, pattern, dir)
}

func (s Store) Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	f, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return grepFiles(f, pattern, dir, glob)
}

// FilesUpdateFrom converts a file map into a FilesUpdate that sets them all.
func FilesUpdateFrom(m map[string]FileData) FilesUpdate {
	u := make(FilesUpdate, len(m))
	for k, v := range m {
		v := v
		u[k] = &v
	}
	return u
}

func init() {
	checkpoint.Register("backends.FileData", FileData{})
	checkpoint.Register("map[string]backends.FileData", map[string]FileData{})
	checkpoint.Register("backends.FilesUpdate", FilesUpdate{})
}
