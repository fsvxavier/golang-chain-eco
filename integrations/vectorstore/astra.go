package vectorstore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Astra stores vectors in a DataStax Astra DB (Apache Cassandra) vector
// collection through the Data API, the supported way to use Cassandra as a
// vector store over HTTP. The same API is served by DataStax HCD / the JSON
// API gateway for self-hosted Cassandra, so Endpoint can point there too.
type Astra struct {
	Endpoint   string // https://<db-id>-<region>.apps.astra.datastax.com
	Token      string // AstraCS:...
	Keyspace   string // default "default_keyspace"
	Collection string
	HTTP       *http.Client
}

func (a *Astra) ks() string {
	if a.Keyspace == "" {
		return "default_keyspace"
	}
	return a.Keyspace
}

func (a *Astra) rest() *rest {
	return &rest{base: a.Endpoint, hc: a.HTTP, headers: map[string]string{"Token": a.Token}}
}

type astraDoc struct {
	ID       string         `json:"_id"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata"`
	Vector   []float32      `json:"$vector"`
	Sim      float64        `json:"$similarity"`
}

// cmd posts one Data API command; the API reports failures with HTTP 200 and
// an "errors" array, which is turned into a Go error here.
func (a *Astra) cmd(ctx context.Context, path string, body map[string]any) ([]astraDoc, error) {
	var resp struct {
		Data struct {
			Documents []astraDoc `json:"documents"`
		} `json:"data"`
		Errors []struct {
			Message   string `json:"message"`
			ErrorCode string `json:"errorCode"`
		} `json:"errors"`
	}
	st, err := a.rest().do(ctx, http.MethodPost, path, body, &resp)
	if err := need404(st, err, "astra "+path); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("astra: %s: %s", resp.Errors[0].ErrorCode, resp.Errors[0].Message)
	}
	return resp.Data.Documents, nil
}

func (a *Astra) path() string {
	return "/api/json/v1/" + url.PathEscape(a.ks()) + "/" + url.PathEscape(a.Collection)
}

func (a *Astra) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if _, err := a.cmd(ctx, "/api/json/v1/"+url.PathEscape(a.ks()), map[string]any{"createCollection": map[string]any{
		"name": a.Collection, "options": map[string]any{"vector": map[string]any{"dimension": len(recs[0].Vector), "metric": "cosine"}}}}); err != nil {
		return err
	}
	for _, r := range recs {
		md := r.Metadata
		if md == nil {
			md = map[string]any{}
		}
		if _, err := a.cmd(ctx, a.path(), map[string]any{"findOneAndReplace": map[string]any{
			"filter":      map[string]any{"_id": r.ID},
			"replacement": map[string]any{"_id": r.ID, "text": r.Text, "metadata": md, "$vector": r.Vector},
			"options":     map[string]any{"upsert": true}}}); err != nil {
			return err
		}
	}
	return nil
}

func (a *Astra) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	docs, err := a.cmd(ctx, a.path(), map[string]any{"find": map[string]any{
		"sort":       map[string]any{"$vector": vec},
		"projection": map[string]any{"$vector": 1, "text": 1, "metadata": 1},
		"options":    map[string]any{"limit": k, "includeSimilarity": true}}})
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(docs))
	for i, d := range docs {
		hits[i] = Hit{Record{d.ID, d.Vector, d.Text, d.Metadata}, d.Sim}
	}
	return hits, nil
}

func (a *Astra) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := a.cmd(ctx, a.path(), map[string]any{"deleteMany": map[string]any{
		"filter": map[string]any{"_id": map[string]any{"$in": ids}}}})
	return err
}
