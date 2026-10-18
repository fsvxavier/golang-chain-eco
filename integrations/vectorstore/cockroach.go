package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CockroachDB stores vectors in CockroachDB's native VECTOR type (v25.2+)
// through database/sql (any PostgreSQL driver, e.g. pgx's stdlib). Search is
// an exact scan ordered by the cosine operator <=>; create a vector index
// yourself on large tables.
type CockroachDB struct {
	DB    *sql.DB
	Table string // default langchain_embedding
}

func (c *CockroachDB) table() (string, error) {
	t := c.Table
	if t == "" {
		t = "langchain_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("cockroachdb: invalid table name %q", t)
	}
	return t, nil
}

func (c *CockroachDB) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := c.table()
	if err != nil {
		return err
	}
	if _, err := c.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id STRING PRIMARY KEY, content STRING NOT NULL, metadata JSONB, embedding VECTOR(%d))`, t, len(recs[0].Vector))); err != nil {
		return err
	}
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := fmt.Sprintf(`UPSERT INTO %s (id, content, metadata, embedding) VALUES ($1, $2, $3::JSONB, $4::VECTOR)`, t)
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := tx.ExecContext(ctx, q, r.ID, r.Text, string(md), vecLiteral(r.Vector)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *CockroachDB) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := c.table()
	if err != nil {
		return nil, err
	}
	rows, err := c.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, content, metadata::STRING, embedding::STRING, 1 - (embedding <=> $1::VECTOR) FROM %s ORDER BY embedding <=> $1::VECTOR LIMIT %s`, t, strconv.Itoa(k)),
		vecLiteral(vec))
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
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

func (c *CockroachDB) Delete(ctx context.Context, ids []string) error {
	t, err := c.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = "$"+strconv.Itoa(i+1), id
	}
	_, err = c.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, t, strings.Join(ph, ",")), args...)
	return err
}
