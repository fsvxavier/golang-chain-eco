package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// MariaDB stores vectors in MariaDB 11.7+ (VECTOR type and VECTOR INDEX)
// through database/sql (ids are ASCII, at most 64 bytes); bring your own driver (github.com/go-sql-driver/mysql).
type MariaDB struct {
	DB    *sql.DB
	Table string // default langchain_embedding
}

func (m *MariaDB) table() (string, error) {
	t := m.Table
	if t == "" {
		t = "langchain_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("mariadb: invalid table name %q", t)
	}
	return t, nil
}

func (m *MariaDB) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := m.table()
	if err != nil {
		return err
	}
	if _, err := m.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY, content LONGTEXT NOT NULL, metadata JSON,
		embedding VECTOR(%d) NOT NULL, VECTOR INDEX (embedding) DISTANCE=cosine)`, t, len(recs[0].Vector))); err != nil {
		return err
	}
	for _, r := range recs { // MariaDB vector indexes cap the primary key at 256 bytes
		if len(r.ID) > 64 {
			return fmt.Errorf("mariadb: id %q longer than 64 bytes", r.ID)
		}
	}
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := fmt.Sprintf(`INSERT INTO %s (id, content, metadata, embedding) VALUES (?, ?, ?, VEC_FromText(?))
		ON DUPLICATE KEY UPDATE content = VALUES(content), metadata = VALUES(metadata), embedding = VALUES(embedding)`, t)
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := tx.ExecContext(ctx, q, r.ID, r.Text, string(md), vecLiteral(r.Vector)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MariaDB) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := m.table()
	if err != nil {
		return nil, err
	}
	rows, err := m.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, content, CAST(metadata AS CHAR), VEC_ToText(embedding), VEC_DISTANCE_COSINE(embedding, VEC_FromText(?)) AS d FROM %s ORDER BY d LIMIT %d`, t, k),
		vecLiteral(vec))
	if err != nil {
		if strings.Contains(err.Error(), "doesn't exist") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var md, emb sql.NullString
		var d float64
		if err := rows.Scan(&h.ID, &h.Text, &md, &emb, &d); err != nil {
			return nil, err
		}
		if md.Valid {
			_ = json.Unmarshal([]byte(md.String), &h.Metadata)
		}
		h.Vector, h.Score = parseVec(emb.String), 1-d
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (m *MariaDB) Delete(ctx context.Context, ids []string) error {
	t, err := m.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err = m.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, t, strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")), args...)
	return err
}
