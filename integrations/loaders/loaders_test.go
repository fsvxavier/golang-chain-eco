package loaders_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/loaders"
	"github.com/fsvxavier/golang-chain-eco/integrations/search"
)

func TestHTMLToText(t *testing.T) {
	title, text := loaders.HTMLToText(`<html><head><title> My &amp; Page </title><style>p{}</style></head><body><script>x()</script><h1>Hi</h1><p>one <b>two</b></p><p>three</p></body></html>`)
	if title != "My & Page" || text != "Hi\none two\nthree" {
		t.Fatalf("%q %q", title, text)
	}
}

func TestWebAndSitemap(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, `<urlset><url><loc>%s/docs/a</loc></url><url><loc>%s/blog/b</loc></url></urlset>`, srv.URL, srv.URL)
		default:
			fmt.Fprint(w, `<title>T</title><p>body</p>`)
		}
	}))
	defer srv.Close()
	f := &search.Fetcher{AllowPrivate: true}
	urls, err := loaders.Sitemap(context.Background(), f, srv.URL+"/sitemap.xml", "/docs/")
	if err != nil || len(urls) != 1 || !strings.HasSuffix(urls[0], "/docs/a") {
		t.Fatal(urls, err)
	}
	docs, err := loaders.Web(f, urls...).Load(context.Background())
	if err != nil || docs[0].PageContent != "body" || docs[0].Metadata["title"] != "T" {
		t.Fatalf("%+v %v", docs, err)
	}
	if _, err := loaders.Web(&search.Fetcher{}, srv.URL).Load(context.Background()); err == nil {
		t.Fatal("SSRF guard must apply to loaders")
	}
}

func TestJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.json")
	_ = os.WriteFile(p, []byte(`{"data":{"items":[{"text":"a","id":1},{"text":"b","id":2}]}}`), 0o600)
	docs, err := loaders.JSON(p, "data.items", "text").Load(context.Background())
	if err != nil || len(docs) != 2 || docs[1].PageContent != "b" || docs[1].Metadata["id"] != float64(2) {
		t.Fatalf("%+v %v", docs, err)
	}
	if _, err := loaders.JSON(p, "data.nope", "text").Load(context.Background()); err == nil {
		t.Fatal("non-array path must fail")
	}
}

func TestUnstructured(t *testing.T) {
	var ctype, key, strategy string
	var fileSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype, key = r.Header.Get("Content-Type"), r.Header.Get("unstructured-api-key")
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			strategy = r.FormValue("strategy")
			f, h, err := r.FormFile("files")
			if err == nil {
				b, _ := io.ReadAll(f)
				fileSeen = h.Filename == "doc.pdf" && string(b) == "%PDF"
			}
		}
		fmt.Fprint(w, `[{"type":"Title","text":"Intro","metadata":{"page_number":1}},{"type":"NarrativeText","text":"Body","metadata":{"page_number":1}},{"type":"Image","text":" "}]`)
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "doc.pdf")
	_ = os.WriteFile(p, []byte("%PDF"), 0o600)
	u := &loaders.Unstructured{URL: srv.URL, APIKey: "uk", Strategy: "fast"}
	docs, err := u.File(p).Load(context.Background())
	if err != nil || len(docs) != 2 || docs[0].Metadata["category"] != "Title" || docs[1].Metadata["page_number"] != float64(1) {
		t.Fatalf("%+v %v", docs, err)
	}
	if !strings.HasPrefix(ctype, "multipart/form-data") || key != "uk" || strategy != "fast" || !fileSeen {
		t.Fatal(ctype, key, strategy, fileSeen)
	}
	u.Single = true
	docs, _ = u.File(p).Load(context.Background())
	if len(docs) != 1 || docs[0].PageContent != "Intro\n\nBody" {
		t.Fatalf("%+v", docs)
	}
}
