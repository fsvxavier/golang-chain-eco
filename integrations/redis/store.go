package redis

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Store implements store.Store: one hash per namespace (key -> envelope) and
// a set listing the namespaces.
type Store struct {
	c      *Client
	Prefix string // default "lcg:store:"
}

var _ store.Store = (*Store)(nil)

// NewStore returns a Redis-backed long-term memory store.
func NewStore(c *Client) *Store { return &Store{c: c, Prefix: "lcg:store:"} }

type envelope struct {
	Value     map[string]any `json:"v"`
	CreatedAt int64          `json:"c"`
	UpdatedAt int64          `json:"u"`
}

func encodeNS(ns []string) string {
	parts := make([]string, len(ns))
	for i, p := range ns {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func decodeNS(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "/")
	for i, p := range parts {
		if u, err := url.PathUnescape(p); err == nil {
			parts[i] = u
		}
	}
	return parts
}

func (s *Store) hash(ns []string) string { return s.Prefix + "h:" + encodeNS(ns) }
func (s *Store) nsSet() string           { return s.Prefix + "ns" }

func (s *Store) Put(ctx context.Context, ns []string, key string, value map[string]any) error {
	now := time.Now().UnixNano()
	env := envelope{Value: value, CreatedAt: now, UpdatedAt: now}
	if old, err := s.Get(ctx, ns, key); err != nil {
		return err
	} else if old != nil {
		env.CreatedAt = old.CreatedAt.UnixNano()
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := s.c.Do(ctx, "HSET", s.hash(ns), key, b); err != nil {
		return err
	}
	_, err = s.c.Do(ctx, "SADD", s.nsSet(), encodeNS(ns))
	return err
}

func (s *Store) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	v, err := s.c.Do(ctx, "HGET", s.hash(ns), key)
	if err != nil || v == nil {
		return nil, err
	}
	return toItem(ns, key, v.(string))
}

func toItem(ns []string, key, raw string) (*store.Item, error) {
	var env envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, err
	}
	return &store.Item{Namespace: ns, Key: key, Value: env.Value,
		CreatedAt: time.Unix(0, env.CreatedAt), UpdatedAt: time.Unix(0, env.UpdatedAt)}, nil
}

func (s *Store) Delete(ctx context.Context, ns []string, key string) error {
	_, err := s.c.Do(ctx, "HDEL", s.hash(ns), key)
	return err
}

// namespaces returns the encoded namespaces under prefix, sorted.
func (s *Store) namespaces(ctx context.Context, prefix []string) ([]string, error) {
	v, err := s.c.Do(ctx, "SMEMBERS", s.nsSet())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range strs(v) {
		d := decodeNS(n)
		if len(d) >= len(prefix) && slices.Equal(d[:len(prefix)], prefix) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	nss, err := s.namespaces(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []store.Item
	for _, n := range nss {
		ns := decodeNS(n)
		v, err := s.c.Do(ctx, "HGETALL", s.hash(ns))
		if err != nil {
			return nil, err
		}
		kv := strs(v)
		var items []store.Item
		for i := 0; i+1 < len(kv); i += 2 {
			it, err := toItem(ns, kv[i], kv[i+1])
			if err != nil {
				return nil, err
			}
			if matches(it.Value, filter) {
				items = append(items, *it)
			}
		}
		sort.Slice(items, func(a, b int) bool { return items[a].Key < items[b].Key })
		out = append(out, items...)
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
	}
	return out, nil
}

func matches(val, filter map[string]any) bool {
	for k, v := range filter {
		a, _ := json.Marshal(val[k])
		b, _ := json.Marshal(v)
		if string(a) != string(b) {
			return false
		}
	}
	return true
}

func (s *Store) ListNamespaces(ctx context.Context, prefix []string) ([][]string, error) {
	nss, err := s.namespaces(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, n := range nss {
		// drop namespaces emptied by Delete
		if v, err := s.c.Do(ctx, "HLEN", s.hash(decodeNS(n))); err == nil && v.(int64) > 0 {
			out = append(out, decodeNS(n))
		}
	}
	return out, nil
}
