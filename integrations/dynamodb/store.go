package dynamodb

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Store implements store.Store. Items: ns (HASH), key (RANGE), value (JSON),
// created_at / updated_at (ns since epoch).
type Store struct {
	c     *Client
	table string
}

var _ store.Store = (*Store)(nil)

// NewStore creates the table if needed and returns the store.
func NewStore(ctx context.Context, c *Client, table string) (*Store, error) {
	if err := c.ensureTable(ctx, table, "ns", "key"); err != nil {
		return nil, err
	}
	return &Store{c: c, table: table}, nil
}

func encodeNS(ns []string) string {
	parts := make([]string, len(ns))
	for i, p := range ns {
		parts[i] = url.PathEscape(p)
	}
	// DynamoDB rejects empty key attributes, so the root namespace is "/".
	return "/" + strings.Join(parts, "/")
}

func decodeNS(s string) []string {
	s = strings.TrimPrefix(s, "/")
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

func (s *Store) key(ns []string, key string) item {
	return item{"ns": sv(encodeNS(ns)), "key": sv(key)}
}

func (s *Store) Put(ctx context.Context, ns []string, key string, value map[string]any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now, created := time.Now().UnixNano(), time.Now().UnixNano()
	if old, err := s.Get(ctx, ns, key); err != nil {
		return err
	} else if old != nil {
		created = old.CreatedAt.UnixNano()
	}
	it := s.key(ns, key)
	it["value"], it["created_at"], it["updated_at"] = sv(string(b)), n(created), n(now)
	_, err = s.c.call(ctx, "PutItem", map[string]any{"TableName": s.table, "Item": it})
	return err
}

func toItem(it item) (store.Item, error) {
	out := store.Item{Namespace: decodeNS(str(it["ns"])), Key: str(it["key"]),
		CreatedAt: time.Unix(0, num(it["created_at"])), UpdatedAt: time.Unix(0, num(it["updated_at"]))}
	return out, json.Unmarshal([]byte(str(it["value"])), &out.Value)
}

func (s *Store) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	out, err := s.c.call(ctx, "GetItem", map[string]any{"TableName": s.table, "Key": s.key(ns, key), "ConsistentRead": true})
	if err != nil || out["Item"] == nil {
		return nil, err
	}
	var raw item
	if err := json.Unmarshal(out["Item"], &raw); err != nil {
		return nil, err
	}
	it, err := toItem(raw)
	return &it, err
}

func (s *Store) Delete(ctx context.Context, ns []string, key string) error {
	_, err := s.c.call(ctx, "DeleteItem", map[string]any{"TableName": s.table, "Key": s.key(ns, key)})
	return err
}

func (s *Store) scan(ctx context.Context, prefix []string) ([]item, error) {
	in := map[string]any{"TableName": s.table, "ConsistentRead": true}
	if len(prefix) > 0 {
		p := encodeNS(prefix)
		in["FilterExpression"] = "ns = :p OR begins_with(ns, :q)"
		in["ExpressionAttributeValues"] = map[string]attr{":p": sv(p), ":q": sv(p + "/")}
	}
	return s.c.pages(ctx, "Scan", in, 0)
}

func (s *Store) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	items, err := s.scan(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []store.Item
	for _, raw := range items {
		it, err := toItem(raw)
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
	items, err := s.scan(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[str(it["ns"])] = true
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([][]string, len(names))
	for i, k := range names {
		out[i] = decodeNS(k)
	}
	return out, nil
}
