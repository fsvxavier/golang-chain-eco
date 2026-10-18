package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PGVector stores vectors in PostgreSQL with the pgvector extension, through
// database/sql (bring your own driver, e.g. pgx's stdlib).
type PGVector struct {
	DB    *sql.DB
	Table string // default langchain_pg_embedding
}

var ident = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (p *PGVector) table() (string, error) {
	t := p.Table
	if t == "" {
		t = "langchain_pg_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("pgvector: invalid table name %q", t)
	}
	return t, nil
}

func vecLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func parseVec(s string) []float32 {
	s = strings.Trim(s, "[] ")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, _ := strconv.ParseFloat(strings.TrimSpace(p), 32)
		out[i] = float32(f)
	}
	return out
}

func (p *PGVector) ensure(ctx context.Context, dim int) error {
	t, err := p.table()
	if err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, content TEXT NOT NULL, metadata JSONB, embedding vector(%d))`, t, dim),
	} {
		if _, err := p.DB.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (p *PGVector) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := p.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	t, _ := p.table()
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := fmt.Sprintf(`INSERT INTO %s (id, content, metadata, embedding) VALUES ($1, $2, $3::jsonb, $4::vector)
		ON CONFLICT (id) DO UPDATE SET content = EXCLUDED.content, metadata = EXCLUDED.metadata, embedding = EXCLUDED.embedding`, t)
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := tx.ExecContext(ctx, q, r.ID, r.Text, string(md), vecLiteral(r.Vector)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PGVector) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := p.table()
	if err != nil {
		return nil, err
	}
	rows, err := p.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, content, metadata::text, embedding::text, 1 - (embedding <=> $1::vector) FROM %s ORDER BY embedding <=> $1::vector LIMIT $2`, t),
		vecLiteral(vec), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var md, emb sql.NullString
		if err := rows.Scan(&h.ID, &h.Text, &md, &emb, &h.Score); err != nil {
			return nil, err
		}
		if md.Valid {
			_ = json.Unmarshal([]byte(md.String), &h.Metadata)
		}
		h.Vector = parseVec(emb.String)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (p *PGVector) Delete(ctx context.Context, ids []string) error {
	t, err := p.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = "$"+strconv.Itoa(i+1), id
	}
	_, err = p.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, t, strings.Join(ph, ",")), args...)
	return err
}
