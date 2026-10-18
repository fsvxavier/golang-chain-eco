package spicedb_test

import (
	"context"
	"os"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/integrations/spicedb"
)

// SPICEDB_URL=http://localhost:8443 SPICEDB_TOKEN=key
// (authzed/spicedb serve --grpc-preshared-key key --http-enabled --datastore-engine memory)
func TestFilterRetriever(t *testing.T) {
	url := os.Getenv("SPICEDB_URL")
	if url == "" {
		t.Skip("SPICEDB_URL not set")
	}
	c, ctx := &spicedb.Client{URL: url, Token: os.Getenv("SPICEDB_TOKEN")}, context.Background()
	if err := c.WriteSchema(ctx, `definition user {}
definition document {
  relation viewer: user
  permission view = viewer
}`); err != nil {
		t.Fatal(err)
	}
	alice := spicedb.Subject{Type: "user", ID: "alice"}
	for _, id := range []string{"d1", "d3"} {
		if err := c.Grant(ctx, spicedb.Ref{Type: "document", ID: id}, "viewer", alice); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := c.Check(ctx, spicedb.Ref{Type: "document", ID: "d2"}, "view", alice); err != nil || ok {
		t.Fatal(ok, err)
	}
	ids, err := c.LookupResources(ctx, "document", "view", alice)
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	inner := retrievers.Func(func(context.Context, string) ([]documents.Document, error) {
		return []documents.Document{
			{PageContent: "one", Metadata: map[string]any{"id": "d1"}},
			{PageContent: "two", Metadata: map[string]any{"id": "d2"}},
			{PageContent: "no id"},
			{PageContent: "three", Metadata: map[string]any{"id": "d3"}},
		}, nil
	})
	safe := spicedb.FilterRetriever(inner, c, alice, "document", "view", func(d documents.Document) string {
		s, _ := d.Metadata["id"].(string)
		return s
	})
	docs, err := safe.Invoke(ctx, "q")
	if err != nil || len(docs) != 2 || docs[0].PageContent != "one" || docs[1].PageContent != "three" {
		t.Fatalf("%+v %v", docs, err)
	}
}
