package mongo

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Saver implements checkpoint.Saver on a collection (default "checkpoints").
// Documents carry checkpoint_ns; the root namespace also matches documents
// written before namespaces existed (field missing).
type Saver struct {
	c          *Client
	Collection string

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// NewSaver returns a MongoDB-backed saver.
func NewSaver(c *Client) *Saver { return &Saver{c: c, Collection: "checkpoints"} }

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

// upsert replaces the document with the given _id.
func (c *Client) upsert(ctx context.Context, coll string, id any, doc M) error {
	_, err := c.Command(ctx, D{{"update", coll}, {"updates", []any{
		D{{"q", D{{"_id", id}}}, {"u", doc}, {"upsert", true}}}}})
	return err
}

func (c *Client) remove(ctx context.Context, coll string, filter any) error {
	_, err := c.Command(ctx, D{{"delete", coll}, {"deletes", []any{D{{"q", filter}, {"limit", int32(0)}}}}})
	return err
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
	err = s.c.upsert(ctx, s.Collection, cp.ThreadID+"/"+cp.ID, M{
		"thread_id": cp.ThreadID, "checkpoint_id": cp.ID, "checkpoint_ns": cp.Namespace,
		"seq": s.nextSeq(), "data": string(b)})
	return cp.ID, err
}

// nsFilter matches one namespace; the root also matches a missing field.
func nsFilter(ns string) any {
	if ns == "" {
		return M{"$in": []any{"", nil}}
	}
	return ns
}

func decode(d M) (checkpoint.Checkpoint, error) {
	raw, _ := d["data"].(string)
	return checkpoint.Unmarshal([]byte(raw))
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	filter := M{"thread_id": cfg.ThreadID, "checkpoint_ns": nsFilter(cfg.Namespace)}
	if cfg.CheckpointID != "" {
		filter["checkpoint_id"] = cfg.CheckpointID
	}
	docs, err := s.c.find(ctx, s.Collection, filter, D{{"seq", int32(-1)}}, 1)
	if err != nil || len(docs) == 0 {
		return checkpoint.Checkpoint{}, false, err
	}
	cp, err := decode(docs[0])
	return cp, err == nil, err
}

func (s *Saver) List(ctx context.Context, threadID string, limit int) ([]checkpoint.Checkpoint, error) {
	docs, err := s.c.find(ctx, s.Collection, M{"thread_id": threadID, "checkpoint_ns": nsFilter("")}, D{{"seq", int32(-1)}}, limit)
	if err != nil {
		return nil, err
	}
	out := make([]checkpoint.Checkpoint, 0, len(docs))
	for _, d := range docs {
		cp, err := decode(d)
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

func (s *Saver) DeleteThread(ctx context.Context, threadID string) error {
	return s.c.remove(ctx, s.Collection, M{"thread_id": threadID})
}
