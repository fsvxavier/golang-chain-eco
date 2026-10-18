package s3

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

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

// objKey: <prefix>/<ns...>/<escaped key>.json — namespace segments and the
// key never contain "/" because PathEscape encodes it.
func (s *Store) objKey(ns []string, key string) string {
	return s.prefix + encodeNS(ns) + "/" + url.PathEscape(key) + ".json"
}

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
	return s.c.Put(ctx, s.bucket, s.objKey(ns, key), b)
}

func (s *Store) read(ctx context.Context, objKey string, ns []string, key string) (*store.Item, error) {
	b, err := s.c.Get(ctx, s.bucket, objKey)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	return &store.Item{Namespace: ns, Key: key, Value: env.Value, CreatedAt: time.Unix(0, env.CreatedAt), UpdatedAt: time.Unix(0, env.UpdatedAt)}, nil
}

func (s *Store) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	return s.read(ctx, s.objKey(ns, key), ns, key)
}

func (s *Store) Delete(ctx context.Context, ns []string, key string) error {
	return s.c.Delete(ctx, s.bucket, s.objKey(ns, key))
}

// under lists the objects whose namespace starts with prefix. The listing
// prefix is segment-aligned ("a/b/") so namespace ["a"] never matches "ab".
func (s *Store) under(ctx context.Context, prefix []string) ([]Object, error) {
	p := s.prefix
	if len(prefix) > 0 {
		p += encodeNS(prefix) + "/"
	}
	return s.c.List(ctx, s.bucket, p)
}

func (s *Store) split(o Object) ([]string, string, bool) {
	rel := strings.TrimPrefix(o.Key, s.prefix)
	i := strings.LastIndex(rel, "/")
	if i < 0 || !strings.HasSuffix(rel, ".json") {
		return nil, "", false
	}
	k, err := url.PathUnescape(strings.TrimSuffix(rel[i+1:], ".json"))
	return decodeNS(rel[:i]), k, err == nil
}

func (s *Store) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	objs, err := s.under(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []store.Item
	for _, o := range objs {
		ns, key, ok := s.split(o)
		if !ok {
			continue
		}
		it, err := s.read(ctx, o.Key, ns, key)
		if err != nil {
			return nil, err
		}
		if it == nil {
			continue
		}
		match := true
		for k, v := range filter {
			a, _ := json.Marshal(it.Value[k])
			b, _ := json.Marshal(v)
			match = match && string(a) == string(b)
		}
		if match {
			out = append(out, *it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
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
	objs, err := s.under(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, o := range objs {
		if ns, _, ok := s.split(o); ok {
			seen[encodeNS(ns)] = true
		}
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
