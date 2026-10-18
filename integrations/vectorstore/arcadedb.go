package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ArcadeDB stores vectors on vertices through the HTTP API and ranks them
// with vectorCosineSimilarity (exact scan, works on every version).
type ArcadeDB struct {
	URL      string // http://localhost:2480
	Database string
	Username string
	Password string
	Type     string // vertex type, default "Chunk"
	HTTP     *http.Client
}

func (a *ArcadeDB) typ() (string, error) {
	t := a.Type
	if t == "" {
		t = "Chunk"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("arcadedb: invalid type name %q", t)
	}
	return t, nil
}

func (a *ArcadeDB) command(ctx context.Context, cmd string, params map[string]any) ([]map[string]any, error) {
	b, _ := json.Marshal(map[string]any{"language": "sql", "command": cmd, "params": params})
	raw, err := post(ctx, a.HTTP, strings.TrimRight(a.URL, "/")+"/api/v1/command/"+url.PathEscape(a.Database), "application/json", b, a.Username, a.Password, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result []map[string]any `json:"result"`
	}
	return out.Result, json.Unmarshal(raw, &out)
}

// ensureDB creates the database when missing (needs the server root user).
func (a *ArcadeDB) ensureDB(ctx context.Context) error {
	b, _ := json.Marshal(map[string]string{"command": "create database " + a.Database})
	_, err := post(ctx, a.HTTP, strings.TrimRight(a.URL, "/")+"/api/v1/server", "application/json", b, a.Username, a.Password, nil)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

func (a *ArcadeDB) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := a.typ()
	if err != nil {
		return err
	}
	if !ident.MatchString(a.Database) {
		return fmt.Errorf("arcadedb: invalid database name %q", a.Database)
	}
	if err := a.ensureDB(ctx); err != nil {
		return err
	}
	for _, c := range []string{"CREATE VERTEX TYPE " + t + " IF NOT EXISTS", "CREATE PROPERTY " + t + ".rid IF NOT EXISTS STRING", "CREATE INDEX IF NOT EXISTS ON " + t + " (rid) UNIQUE"} {
		if _, err := a.command(ctx, c, nil); err != nil {
			return err
		}
	}
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := a.command(ctx, "UPDATE "+t+" SET rid = :rid, text = :text, metadata = :md, embedding = :emb UPSERT WHERE rid = :rid",
			map[string]any{"rid": r.ID, "text": r.Text, "md": string(md), "emb": r.Vector}); err != nil {
			return err
		}
	}
	return nil
}

func (a *ArcadeDB) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := a.typ()
	if err != nil {
		return nil, err
	}
	rows, err := a.command(ctx, fmt.Sprintf("SELECT rid, text, metadata, embedding, vectorCosineSimilarity(embedding, :q) AS score FROM %s ORDER BY score DESC LIMIT %d", t, k), map[string]any{"q": vec})
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		return nil, err
	}
	var hits []Hit
	for _, r := range rows {
		var h Hit
		h.ID, _ = r["rid"].(string)
		h.Text, _ = r["text"].(string)
		md, _ := r["metadata"].(string)
		_ = json.Unmarshal([]byte(md), &h.Metadata)
		h.Score, _ = r["score"].(float64)
		if emb, ok := r["embedding"].([]any); ok {
			for _, f := range emb {
				x, _ := f.(float64)
				h.Vector = append(h.Vector, float32(x))
			}
		}
		hits = append(hits, h)
	}
	return hits, nil
}

func (a *ArcadeDB) Delete(ctx context.Context, ids []string) error {
	t, err := a.typ()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = a.command(ctx, "DELETE FROM "+t+" WHERE rid IN :ids", map[string]any{"ids": ids})
	return err
}
