package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Weaviate stores vectors in a Weaviate class (REST + GraphQL, bring-your-own
// vectors). Class names must start with an uppercase letter.
type Weaviate struct {
	URL    string
	APIKey string
	Class  string
	HTTP   *http.Client
}

func (w *Weaviate) rest() *rest {
	h := map[string]string{}
	if w.APIKey != "" {
		h["Authorization"] = "Bearer " + w.APIKey
	}
	return &rest{base: w.URL, hc: w.HTTP, headers: h}
}

func (w *Weaviate) ensure(ctx context.Context) error {
	r := w.rest()
	st, err := r.do(ctx, http.MethodGet, "/v1/schema/"+url.PathEscape(w.Class), nil, nil)
	if err != nil || st != http.StatusNotFound {
		return err
	}
	_, err = r.do(ctx, http.MethodPost, "/v1/schema", map[string]any{
		"class": w.Class, "vectorizer": "none",
		"properties": []map[string]any{
			{"name": "text", "dataType": []string{"text"}},
			{"name": "metadata", "dataType": []string{"text"}},
			{"name": "docId", "dataType": []string{"text"}},
		},
	}, nil)
	return err
}

func (w *Weaviate) Upsert(ctx context.Context, recs []Record) error {
	if err := w.ensure(ctx); err != nil {
		return err
	}
	objs := make([]map[string]any, len(recs))
	for i, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		objs[i] = map[string]any{"class": w.Class, "id": pointID(r.ID), "vector": r.Vector,
			"properties": map[string]any{"text": r.Text, "metadata": string(md), "docId": r.ID}}
	}
	var res []struct {
		Result struct {
			Errors *struct {
				Error []struct{ Message string } `json:"error"`
			} `json:"errors"`
		} `json:"result"`
	}
	if _, err := w.rest().do(ctx, http.MethodPost, "/v1/batch/objects", map[string]any{"objects": objs}, &res); err != nil {
		return err
	}
	for _, r := range res {
		if r.Result.Errors != nil && len(r.Result.Errors.Error) > 0 {
			return fmt.Errorf("weaviate: %s", r.Result.Errors.Error[0].Message)
		}
	}
	return nil
}

func (w *Weaviate) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	vb, _ := json.Marshal(vec)
	q := fmt.Sprintf(`{Get{%s(nearVector:{vector:%s},limit:%d){text metadata docId _additional{distance vector}}}}`, w.Class, vb, k)
	var out struct {
		Data struct {
			Get map[string][]struct {
				Text     string `json:"text"`
				Metadata string `json:"metadata"`
				DocID    string `json:"docId"`
				Add      struct {
					Distance float64   `json:"distance"`
					Vector   []float32 `json:"vector"`
				} `json:"_additional"`
			} `json:"Get"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if _, err := w.rest().do(ctx, http.MethodPost, "/v1/graphql", map[string]any{"query": q}, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		if strings.Contains(out.Errors[0].Message, "Cannot query field") {
			return nil, nil // class not created yet
		}
		return nil, fmt.Errorf("weaviate: %s", out.Errors[0].Message)
	}
	items := out.Data.Get[w.Class]
	hits := make([]Hit, len(items))
	for i, x := range items {
		var md map[string]any
		_ = json.Unmarshal([]byte(x.Metadata), &md)
		hits[i] = Hit{Record{x.DocID, x.Add.Vector, x.Text, md}, 1 - x.Add.Distance}
	}
	return hits, nil
}

func (w *Weaviate) Delete(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := w.rest().do(ctx, http.MethodDelete, "/v1/objects/"+url.PathEscape(w.Class)+"/"+pointID(id), nil, nil); err != nil {
			return err
		}
	}
	return nil
}
