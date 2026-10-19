package s3

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Saver implements checkpoint.Saver: one object per checkpoint at
// <prefix><thread>/<seq>_<id>.json, so listing a thread returns them in order.
// A subgraph namespace adds one directory: <prefix><thread>/<ns>/<seq>_<id>.json.
// S3 has no atomic append, but keys are unique per checkpoint so concurrent
// writers never overwrite each other.
type Saver struct {
	c              *Client
	bucket, prefix string

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// NewSaver returns a saver writing under prefix in bucket (create the bucket
// first, or call Client.EnsureBucket).
func NewSaver(c *Client, bucket, prefix string) *Saver {
	return &Saver{c: c, bucket: bucket, prefix: prefix}
}

// dir is the prefix of one (thread, namespace); the root namespace has no
// subdirectory.
func (s *Saver) dir(thread, ns string) string {
	d := s.prefix + url.PathEscape(thread) + "/"
	if ns != "" {
		d += url.PathEscape(ns) + "/"
	}
	return d
}

func (s *Saver) nextSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := time.Now().UnixNano()
	if n <= s.last {
		n = s.last + 1
	}
	s.last = n
	return n
}

func (s *Saver) Put(ctx context.Context, cp checkpoint.Checkpoint) (string, error) {
	if cp.ThreadID == "" {
		return "", errors.New("checkpoint: empty thread id")
	}
	if cp.ID == "" {
		cp.ID = messages.NewID()
	}
	b, err := checkpoint.Marshal(cp)
	if err != nil {
		return "", err
	}
	return cp.ID, s.c.Put(ctx, s.bucket, fmt.Sprintf("%s%020d_%s.json", s.dir(cp.ThreadID, cp.Namespace), s.nextSeq(), url.PathEscape(cp.ID)), b)
}

// keys lists one namespace's checkpoint objects, oldest first. The root
// listing skips the subgraph directories below the thread.
func (s *Saver) keys(ctx context.Context, thread, ns string) ([]string, error) {
	dir := s.dir(thread, ns)
	objs, err := s.c.List(ctx, s.bucket, dir)
	var keys []string
	for _, o := range objs {
		if ns == "" && strings.Contains(strings.TrimPrefix(o.Key, dir), "/") {
			continue
		}
		keys = append(keys, o.Key)
	}
	return keys, err // S3 lists in ascending key order = oldest first
}

func (s *Saver) load(ctx context.Context, key string) (checkpoint.Checkpoint, error) {
	b, err := s.c.Get(ctx, s.bucket, key)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	return checkpoint.Unmarshal(b)
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	keys, err := s.keys(ctx, cfg.ThreadID, cfg.Namespace)
	if err != nil || len(keys) == 0 {
		return checkpoint.Checkpoint{}, false, err
	}
	key := keys[len(keys)-1]
	if cfg.CheckpointID != "" {
		key = ""
		for _, k := range keys {
			if strings.HasSuffix(k, "_"+url.PathEscape(cfg.CheckpointID)+".json") {
				key = k
			}
		}
		if key == "" {
			return checkpoint.Checkpoint{}, false, nil
		}
	}
	cp, err := s.load(ctx, key)
	return cp, err == nil, err
}

func (s *Saver) List(ctx context.Context, threadID string, limit int) ([]checkpoint.Checkpoint, error) {
	keys, err := s.keys(ctx, threadID, "")
	if err != nil {
		return nil, err
	}
	slices.Reverse(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]checkpoint.Checkpoint, 0, len(keys))
	for _, k := range keys {
		cp, err := s.load(ctx, k)
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

func (s *Saver) DeleteThread(ctx context.Context, threadID string) error {
	// Every namespace lives under the thread's directory, so one listing
	// finds them all.
	objs, err := s.c.List(ctx, s.bucket, s.dir(threadID, ""))
	if err != nil {
		return err
	}
	for _, o := range objs {
		if err := s.c.Delete(ctx, s.bucket, o.Key); err != nil {
			return err
		}
	}
	return nil
}

// Loader returns a documents.Loader reading every object under prefix as a
// UTF-8 text document (metadata: source, key, size).
func Loader(c *Client, bucket, prefix string) documents.Loader {
	return documents.LoaderFunc(func(ctx context.Context) ([]documents.Document, error) {
		objs, err := c.List(ctx, bucket, prefix)
		if err != nil {
			return nil, err
		}
		var docs []documents.Document
		for _, o := range objs {
			if strings.HasSuffix(o.Key, "/") {
				continue
			}
			b, err := c.Get(ctx, bucket, o.Key)
			if err != nil {
				return nil, err
			}
			docs = append(docs, documents.Document{PageContent: string(b),
				Metadata: map[string]any{"source": "s3://" + bucket + "/" + o.Key, "key": o.Key, "size": o.Size}})
		}
		return docs, nil
	})
}

// Store implements store.Store with one JSON object per item at
// <prefix><encoded namespace>/<key>.json.
type Store struct {
	c              *Client
	bucket, prefix string
}

var _ store.Store = (*Store)(nil)

// NewStore returns an S3-backed long-term memory store.
func NewStore(c *Client, bucket, prefix string) *Store {
	return &Store{c: c, bucket: bucket, prefix: prefix}
}
