package redis

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Saver implements checkpoint.Saver. Per thread it keeps a hash
// (id -> serialised checkpoint) and a sorted set ordering the ids.
type Saver struct {
	c      *Client
	Prefix string // key prefix, default "lcg:ckpt:"

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// NewSaver returns a Redis-backed saver.
func NewSaver(c *Client) *Saver { return &Saver{c: c, Prefix: "lcg:ckpt:"} }

func (s *Saver) data(t string) string { return s.Prefix + "data:" + t }
func (s *Saver) idx(t string) string  { return s.Prefix + "idx:" + t }

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
	if _, err := s.c.Do(ctx, "HSET", s.data(cp.ThreadID), cp.ID, b); err != nil {
		return "", err
	}
	// score is a float64: UnixMicro stays exact (< 2^53) and still orders puts
	_, err = s.c.Do(ctx, "ZADD", s.idx(cp.ThreadID), s.nextSeq()/1000, cp.ID)
	return cp.ID, err
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	id := cfg.CheckpointID
	if id == "" {
		v, err := s.c.Do(ctx, "ZREVRANGE", s.idx(cfg.ThreadID), 0, 0)
		if err != nil {
			return checkpoint.Checkpoint{}, false, err
		}
		ids := strs(v)
		if len(ids) == 0 {
			return checkpoint.Checkpoint{}, false, nil
		}
		id = ids[0]
	}
	v, err := s.c.Do(ctx, "HGET", s.data(cfg.ThreadID), id)
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
	v, err := s.c.Do(ctx, "ZREVRANGE", s.idx(threadID), 0, stop)
	if err != nil {
		return nil, err
	}
	ids := strs(v)
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{"HMGET", s.data(threadID)}
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
	_, err := s.c.Do(ctx, "DEL", s.data(threadID), s.idx(threadID))
	return err
}
