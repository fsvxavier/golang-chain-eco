package aws_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/aws"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

func TestAWS(t *testing.T) {
	var path, target, auth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, target, auth = r.URL.Path, r.Header.Get("X-Amz-Target"), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(b, &body)
		switch {
		case strings.HasSuffix(path, "/retrieve"):
			w.Write([]byte(`{"retrievalResults":[{"content":{"text":"kb passage"},"score":0.7,"location":{"type":"S3"},"metadata":{"k":"v"}}]}`))
		case target == "AWSKendraFrontendService.Query":
			w.Write([]byte(`{"ResultItems":[{"Id":"i1","Type":"ANSWER","DocumentTitle":{"Text":"T"},"DocumentExcerpt":{"Text":"excerpt"},"DocumentURI":"https://x"}]}`))
		case strings.Contains(path, "/invocations"):
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	c := &aws.Client{Region: "eu-west-1", Endpoint: srv.URL, Credentials: bedrock.Credentials{AccessKeyID: "AK", SecretAccessKey: "SK"}}
	ctx := context.Background()

	docs, err := c.KnowledgeBase("KB123", 4).Invoke(ctx, "what is x")
	if err != nil || len(docs) != 1 || docs[0].PageContent != "kb passage" || docs[0].Metadata["k"] != "v" || path != "/knowledgebases/KB123/retrieve" {
		t.Fatalf("%+v %v %s", docs, err, path)
	}
	vsc := body["retrievalConfiguration"].(map[string]any)["vectorSearchConfiguration"].(map[string]any)
	if body["retrievalQuery"].(map[string]any)["text"] != "what is x" || vsc["numberOfResults"].(float64) != 4 {
		t.Fatal(body)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AK/") || !strings.Contains(auth, "/eu-west-1/bedrock/aws4_request") {
		t.Fatal(auth)
	}

	if docs, err = c.Kendra("idx", 3).Invoke(ctx, "q"); err != nil || len(docs) != 1 || docs[0].PageContent != "excerpt" || docs[0].Metadata["title"] != "T" || body["IndexId"] != "idx" {
		t.Fatalf("%+v %v", docs, err)
	}
	if !strings.Contains(auth, "/kendra/aws4_request") {
		t.Fatal(auth)
	}

	out, err := c.LambdaTool("run_report", "Runs the report", "my-fn").Call(ctx, map[string]any{"payload": map[string]any{"day": "mon"}})
	if err != nil || out != `{"ok":true}` || path != "/2015-03-31/functions/my-fn/invocations" || body["day"] != "mon" {
		t.Fatal(out, err, path, body)
	}
}
