package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Oracle stores vectors in Oracle Database 23ai (AI Vector Search) through
// database/sql; bring your own driver (github.com/sijms/go-ora/v2 or godror).
// Search is an exact scan with VECTOR_DISTANCE(..., COSINE). Returned hits
// carry no embedding (MMR re-embeds the texts).
type Oracle struct {
	DB    *sql.DB
	Table string // default langchain_embedding
}

func (o *Oracle) table() (string, error) {
	t := o.Table
	if t == "" {
		t = "langchain_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("oracle: invalid table name %q", t)
	}
	return t, nil
}

func (o *Oracle) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := o.table()
	if err != nil {
		return err
	}
	// ORA-00955 = name already used: the table exists
	if _, err := o.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (id VARCHAR2(255) PRIMARY KEY, content CLOB, metadata CLOB, embedding VECTOR(%d, FLOAT32))`, t, len(recs[0].Vector))); err != nil &&
		!strings.Contains(err.Error(), "ORA-00955") {
		return err
	}
	q := fmt.Sprintf(`MERGE INTO %s t USING (SELECT :1 id, :2 content, :3 metadata, TO_VECTOR(:4) embedding FROM dual) s ON (t.id = s.id)
		WHEN MATCHED THEN UPDATE SET t.content = s.content, t.metadata = s.metadata, t.embedding = s.embedding
		WHEN NOT MATCHED THEN INSERT (id, content, metadata, embedding) VALUES (s.id, s.content, s.metadata, s.embedding)`, t)
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := o.DB.ExecContext(ctx, q, r.ID, r.Text, string(md), vecLiteral(r.Vector)); err != nil {
			return err
		}
	}
	return nil
}

func (o *Oracle) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := o.table()
	if err != nil {
		return nil, err
	}
	rows, err := o.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, content, metadata, 1 - VECTOR_DISTANCE(embedding, TO_VECTOR(:1), COSINE) AS score FROM %s ORDER BY score DESC FETCH FIRST %d ROWS ONLY`, t, k), vecLiteral(vec))
	if err != nil {
		if strings.Contains(err.Error(), "ORA-00942") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var content, md sql.NullString
		if err := rows.Scan(&h.ID, &content, &md, &h.Score); err != nil {
			return nil, err
		}
		h.Text = content.String
		_ = json.Unmarshal([]byte(md.String), &h.Metadata)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (o *Oracle) Delete(ctx context.Context, ids []string) error {
	t, err := o.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = fmt.Sprintf(":%d", i+1), id
	}
	_, err = o.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, t, strings.Join(ph, ",")), args...)
	return err
}
