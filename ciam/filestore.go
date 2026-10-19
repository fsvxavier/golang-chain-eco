package ciam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// FileStore is a store.Store that keeps everything in memory and persists it
// to one JSON file on Save. It makes the index survive between CLI runs
// without a database. Use a real backend (sqlstore, redis, mongo) for large
// projects or concurrent writers.
type FileStore struct {
	*store.Memory
	path string
	mu   sync.Mutex
}

type fileItem struct {
	Namespace []string       `json:"namespace"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	Created   time.Time      `json:"created"`
	Updated   time.Time      `json:"updated"`
}

// OpenFileStore loads path if it exists (a missing file is an empty store).
func OpenFileStore(path string) (*FileStore, error) {
	fs := &FileStore{Memory: store.NewMemory(), path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fs, nil
	}
	if err != nil {
		return nil, err
	}
	var items []fileItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("ciam: %s is not a valid store file: %w", path, err)
	}
	for _, it := range items {
		if err := fs.Memory.Put(context.Background(), it.Namespace, it.Key, it.Value); err != nil {
			return nil, err
		}
	}
	return fs, nil
}

// Save writes the store atomically (temp file + rename).
func (f *FileStore) Save() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	items, err := f.Memory.Search(context.Background(), nil, nil, 0)
	if err != nil {
		return err
	}
	out := make([]fileItem, len(items))
	for i, it := range items {
		out[i] = fileItem{it.Namespace, it.Key, it.Value, it.CreatedAt, it.UpdatedAt}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".ciam-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

// saver is implemented by stores that persist on demand.
type saver interface{ Save() error }
