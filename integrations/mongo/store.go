package mongo

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Store implements store.Store on a collection (default "store_items").
// Values are kept as JSON text so any Go value round-trips.
type Store struct {
	c          *Client
	Collection string
}

var _ store.Store = (*Store)(nil)

// NewStore returns a MongoDB-backed long-term memory store.
func NewStore(c *Client) *Store { return &Store{c: c, Collection: "store_items"} }

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

func (s *Store) Put(ctx context.Context, ns []string, key string, value map[string]any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now, created := time.Now().UnixNano(), int64(0)
	if old, err := s.Get(ctx, ns, key); err != nil {
		return err
	} else if old != nil {
		created = old.CreatedAt.UnixNano()
	} else {
		created = now
	}
	e := encodeNS(ns)
	return s.c.upsert(ctx, s.Collection, e+"\x00"+key, M{"ns": e, "key": key, "value": string(b), "created_at": created, "updated_at": now})
}

func toItem(d M) (store.Item, error) {
	it := store.Item{Key: d["key"].(string), CreatedAt: time.Unix(0, int64(num(d["created_at"]))), UpdatedAt: time.Unix(0, int64(num(d["updated_at"])))}
	it.Namespace = decodeNS(d["ns"].(string))
	return it, json.Unmarshal([]byte(d["value"].(string)), &it.Value)
}

func (s *Store) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	docs, err := s.c.find(ctx, s.Collection, M{"_id": encodeNS(ns) + "\x00" + key}, nil, 1)
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	it, err := toItem(docs[0])
	return &it, err
}

func (s *Store) Delete(ctx context.Context, ns []string, key string) error {
	return s.c.remove(ctx, s.Collection, M{"_id": encodeNS(ns) + "\x00" + key})
}

func (s *Store) under(ctx context.Context, prefix []string) ([]M, error) {
	filter := M{}
	if p := encodeNS(prefix); p != "" {
		filter["ns"] = M{"$regex": "^" + regexp.QuoteMeta(p) + "(/|$)"}
	}
	return s.c.find(ctx, s.Collection, filter, nil, 0)
}

func (s *Store) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	docs, err := s.under(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []store.Item
	for _, d := range docs {
		it, err := toItem(d)
		if err != nil {
			return nil, err
		}
		ok := true
		for k, v := range filter {
			a, _ := json.Marshal(it.Value[k])
			b, _ := json.Marshal(v)
			ok = ok && string(a) == string(b)
		}
		if ok {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := encodeNS(out[i].Namespace), encodeNS(out[j].Namespace); a != b {
			return a < b
		}
		return out[i].Key < out[j].Key
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) ListNamespaces(ctx context.Context, prefix []string) ([][]string, error) {
	docs, err := s.under(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, d := range docs {
		seen[d["ns"].(string)] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([][]string, len(names))
	for i, n := range names {
		out[i] = decodeNS(n)
	}
	return out, nil
}
