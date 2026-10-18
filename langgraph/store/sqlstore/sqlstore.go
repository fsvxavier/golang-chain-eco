// Package sqlstore is a store.Store on database/sql for SQLite and
// PostgreSQL (bring your own driver), giving long-term memory that survives
// restarts and is shared across processes.
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Dialect selects the placeholder style.
type Dialect int

const (
	SQLite Dialect = iota
	Postgres
)

// Store implements store.Store.
type Store struct {
	db *sql.DB
	d  Dialect
}

var _ store.Store = (*Store)(nil)

// New creates the store and its table (store_items_go) if needed.
func New(ctx context.Context, db *sql.DB, d Dialect) (*Store, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS store_items_go (
		ns TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
		created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, PRIMARY KEY (ns, key))`)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: create table: %w", err)
	}
	return &Store{db: db, d: d}, nil
}

func (s *Store) q(query string) string {
	if s.d != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
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

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Store) Put(ctx context.Context, ns []string, key string, value map[string]any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().UnixNano()
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO store_items_go (ns, key, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (ns, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`),
		encodeNS(ns), key, string(b), now, now)
	return err
}

func (s *Store) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	var val string
	var c, u int64
	err := s.db.QueryRowContext(ctx, s.q(`SELECT value, created_at, updated_at FROM store_items_go WHERE ns = ? AND key = ?`), encodeNS(ns), key).Scan(&val, &c, &u)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it := &store.Item{Namespace: ns, Key: key, CreatedAt: time.Unix(0, c), UpdatedAt: time.Unix(0, u)}
	return it, json.Unmarshal([]byte(val), &it.Value)
}

func (s *Store) Delete(ctx context.Context, ns []string, key string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM store_items_go WHERE ns = ? AND key = ?`), encodeNS(ns), key)
	return err
}

func (s *Store) rows(ctx context.Context, prefix []string) (*sql.Rows, error) {
	p := encodeNS(prefix)
	if p == "" {
		return s.db.QueryContext(ctx, `SELECT ns, key, value, created_at, updated_at FROM store_items_go ORDER BY ns, key`)
	}
	return s.db.QueryContext(ctx, s.q(`SELECT ns, key, value, created_at, updated_at FROM store_items_go
		WHERE ns = ? OR ns LIKE ? ESCAPE '\' ORDER BY ns, key`), p, likeEscape(p)+"/%")
}

func (s *Store) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	rows, err := s.rows(ctx, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Item
	for rows.Next() {
		var ns, key, val string
		var c, u int64
		if err := rows.Scan(&ns, &key, &val, &c, &u); err != nil {
			return nil, err
		}
		it := store.Item{Namespace: decodeNS(ns), Key: key, CreatedAt: time.Unix(0, c), UpdatedAt: time.Unix(0, u)}
		if err := json.Unmarshal([]byte(val), &it.Value); err != nil {
			return nil, err
		}
		match := true
		for k, v := range filter {
			// compare after a JSON round trip so numbers match regardless of Go type
			a, _ := json.Marshal(it.Value[k])
			b, _ := json.Marshal(v)
			if string(a) != string(b) {
				match = false
				break
			}
		}
		if match {
			out = append(out, it)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, rows.Err()
}

func (s *Store) ListNamespaces(ctx context.Context, prefix []string) ([][]string, error) {
	rows, err := s.rows(ctx, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var ns, key, val string
		var c, u int64
		if err := rows.Scan(&ns, &key, &val, &c, &u); err != nil {
			return nil, err
		}
		seen[ns] = true
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
	return out, rows.Err()
}
