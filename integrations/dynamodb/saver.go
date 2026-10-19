package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Saver implements checkpoint.Saver. Items: thread_id (HASH), sk =
// "<seq>#<id>" (RANGE, so a reverse Query yields newest first), checkpoint_id,
// checkpoint_ns (missing on items written before namespaces), data (JSON).
type Saver struct {
	c     *Client
	table string

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// NewSaver creates the table if needed and returns the saver.
func NewSaver(ctx context.Context, c *Client, table string) (*Saver, error) {
	if err := c.ensureTable(ctx, table, "thread_id", "sk"); err != nil {
		return nil, err
	}
	return &Saver{c: c, table: table}, nil
}

func (s *Saver) nextSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := time.Now().UnixNano()
	if v <= s.last {
		v = s.last + 1
	}
	s.last = v
	return v
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
	_, err = s.c.call(ctx, "PutItem", map[string]any{"TableName": s.table, "Item": item{
		"thread_id": sv(cp.ThreadID), "sk": sv(fmt.Sprintf("%020d#%s", s.nextSeq(), cp.ID)),
		"checkpoint_id": sv(cp.ID), "checkpoint_ns": sv(cp.Namespace), "data": sv(string(b))}})
	return cp.ID, err
}

func (s *Saver) query(ctx context.Context, threadID, ns, filterID string, limit int) ([]checkpoint.Checkpoint, error) {
	in := map[string]any{"TableName": s.table, "ScanIndexForward": false,
		"KeyConditionExpression":    "thread_id = :t",
		"ExpressionAttributeValues": map[string]attr{":t": sv(threadID)}}
	// The root namespace also matches items without the attribute (older rows).
	vals := in["ExpressionAttributeValues"].(map[string]attr)
	vals[":n"] = sv(ns)
	filter := "checkpoint_ns = :n"
	if ns == "" {
		filter = "(attribute_not_exists(checkpoint_ns) OR checkpoint_ns = :n)"
	}
	if filterID != "" {
		filter += " AND checkpoint_id = :c"
		vals[":c"] = sv(filterID)
	}
	in["FilterExpression"] = filter
	if limit > 0 {
		in["Limit"] = limit // per page: pages() reads on until limit matches
	}
	items, err := s.c.pages(ctx, "Query", in, limit)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]checkpoint.Checkpoint, 0, len(items))
	for _, it := range items {
		cp, err := checkpoint.Unmarshal([]byte(str(it["data"])))
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	cps, err := s.query(ctx, cfg.ThreadID, cfg.Namespace, cfg.CheckpointID, 1)
	if err != nil || len(cps) == 0 {
		return checkpoint.Checkpoint{}, false, err
	}
	return cps[0], true, nil
}

func (s *Saver) List(ctx context.Context, threadID string, limit int) ([]checkpoint.Checkpoint, error) {
	return s.query(ctx, threadID, "", "", limit)
}

func (s *Saver) DeleteThread(ctx context.Context, threadID string) error {
	items, err := s.c.pages(ctx, "Query", map[string]any{"TableName": s.table,
		"KeyConditionExpression": "thread_id = :t", "ProjectionExpression": "thread_id, sk",
		"ExpressionAttributeValues": map[string]attr{":t": sv(threadID)}}, 0)
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := s.c.call(ctx, "DeleteItem", map[string]any{"TableName": s.table, "Key": item{"thread_id": it["thread_id"], "sk": it["sk"]}}); err != nil {
			return err
		}
	}
	return nil
}
