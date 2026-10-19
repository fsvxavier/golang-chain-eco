package redis

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Saver implements checkpoint.Saver. Per (thread, namespace) it keeps a hash
// (id -> serialised checkpoint) and a sorted set ordering the ids; a set per
// thread lists its namespaces so DeleteThread can remove them all.
type Saver struct {
	c      *Client
	Prefix string // key prefix, default "lcg:ckpt:"

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// NewSaver returns a Redis-backed saver.
func NewSaver(c *Client) *Saver { return &Saver{c: c, Prefix: "lcg:ckpt:"} }

// slot names the storage of (thread, namespace). The root namespace keeps the
// plain thread name, so keys written before namespaces existed still match.
func slot(thread, ns string) string {
	if ns == "" {
		return thread
	}
	return thread + "\x1f" + ns
}

func (s *Saver) data(k string) string       { return s.Prefix + "data:" + k }
func (s *Saver) idx(k string) string        { return s.Prefix + "idx:" + k }
func (s *Saver) nsSet(thread string) string { return s.Prefix + "ns:" + thread }

func (s *Saver) nextSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := time.Now().UnixNano()
	if n <= s.last {
		n = s.last + 1000 // keep distinct after /1000
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
	k := slot(cp.ThreadID, cp.Namespace)
	if _, err := s.c.Do(ctx, "HSET", s.data(k), cp.ID, b); err != nil {
		return "", err
	}
	// score is a float64: UnixMicro stays exact (< 2^53) and still orders puts
	if _, err := s.c.Do(ctx, "ZADD", s.idx(k), s.nextSeq()/1000, cp.ID); err != nil {
		return "", err
	}
	if cp.Namespace != "" {
		_, err = s.c.Do(ctx, "SADD", s.nsSet(cp.ThreadID), cp.Namespace)
	}
	return cp.ID, err
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	id, k := cfg.CheckpointID, slot(cfg.ThreadID, cfg.Namespace)
	if id == "" {
		v, err := s.c.Do(ctx, "ZREVRANGE", s.idx(k), 0, 0)
		if err != nil {
			return checkpoint.Checkpoint{}, false, err
		}
		ids := strs(v)
		if len(ids) == 0 {
			return checkpoint.Checkpoint{}, false, nil
		}
		id = ids[0]
	}
	v, err := s.c.Do(ctx, "HGET", s.data(k), id)
	if err != nil || v == nil {
		return checkpoint.Checkpoint{}, false, err
	}
	cp, err := checkpoint.Unmarshal([]byte(v.(string)))
	return cp, err == nil, err
}

func (s *Saver) List(ctx context.Context, threadID string, limit int) ([]checkpoint.Checkpoint, error) {
	stop := -1
	if limit > 0 {
		stop = limit - 1
	}
	k := slot(threadID, "")
	v, err := s.c.Do(ctx, "ZREVRANGE", s.idx(k), 0, stop)
	if err != nil {
		return nil, err
	}
	ids := strs(v)
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{"HMGET", s.data(k)}
	for _, id := range ids {
		args = append(args, id)
	}
	v, err = s.c.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	var out []checkpoint.Checkpoint
	for _, e := range v.([]any) {
		raw, ok := e.(string)
		if !ok {
			continue
		}
		cp, err := checkpoint.Unmarshal([]byte(raw))
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

func (s *Saver) DeleteThread(ctx context.Context, threadID string) error {
	members, err := s.c.Do(ctx, "SMEMBERS", s.nsSet(threadID))
	if err != nil {
		return err
	}
	args := []any{"DEL", s.nsSet(threadID), s.data(slot(threadID, "")), s.idx(slot(threadID, ""))}
	for _, ns := range strs(members) {
		args = append(args, s.data(slot(threadID, ns)), s.idx(slot(threadID, ns)))
	}
	_, err = s.c.Do(ctx, args...)
	return err
}
