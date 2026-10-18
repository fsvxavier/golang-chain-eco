package search_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/search"
)

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestEngines(t *testing.T) {
	ctx := context.Background()
	var gotAuth, gotBody string
	mk := func(body string) *httptest.Server {
		return serve(t, func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization") + r.Header.Get("x-api-key") + r.Header.Get("X-API-KEY") + r.Header.Get("X-Subscription-Token")
			b := new(strings.Builder)
			_, _ = fmt.Fprint(b, r.URL.RequestURI())
			gotBody = b.String()
			fmt.Fprint(w, body)
		})
	}
	check := func(name string, s search.Searcher, wantTitle string) {
		t.Helper()
		rs, err := s.Search(ctx, "go generics", 3)
		if err != nil || len(rs) == 0 || rs[len(rs)-1].Title != wantTitle {
			t.Fatalf("%s: %+v %v", name, rs, err)
		}
	}

	tav := search.NewTavily("tk")
	tav.URL = mk(`{"results":[{"title":"T1","url":"u","content":"c","score":0.9}]}`).URL
	check("tavily", tav, "T1")
	if gotAuth != "Bearer tk" {
		t.Fatal(gotAuth)
	}

	exa := &search.Exa{}
	exa.URL, exa.APIKey = mk(`{"results":[{"title":"E1","url":"u","text":"c"}]}`).URL, "ek"
	check("exa", exa, "E1")
	if !strings.Contains(gotAuth, "ek") {
		t.Fatal(gotAuth)
	}

	br := &search.Brave{}
	br.URL, br.APIKey = mk(`{"web":{"results":[{"title":"<b>B1</b>","url":"u","description":"d &amp; e"}]}}`).URL, "bk"
	check("brave", br, "B1")
	if !strings.Contains(gotBody, "q=go+generics") && !strings.Contains(gotBody, "q=go%20generics") {
		t.Fatal(gotBody)
	}

	sp := &search.Serper{}
	sp.URL, sp.APIKey = mk(`{"answerBox":{"answer":"42"},"organic":[{"title":"S1","link":"u","snippet":"s"}]}`).URL, "sk"
	check("serper", sp, "S1")

	sa := &search.SerpAPI{}
	sa.URL, sa.APIKey = mk(`{"organic_results":[{"title":"A1","link":"u","snippet":"s"}]}`).URL, "ak"
	check("serpapi", sa, "A1")

	sx := &search.SearXNG{}
	sx.URL = mk(`{"results":[{"title":"X1","url":"u","content":"c"},{"title":"X2","url":"u","content":"c"}]}`).URL
	if rs, _ := sx.Search(ctx, "q", 1); len(rs) != 1 {
		t.Fatal("searxng must honour max")
	}

	ddg := &search.DuckDuckGo{}
	ddg.URL = mk(`<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F&rut=x">The <b>Go</b> site</a><a class="result__snippet" href="x">Build <b>simple</b> software</a>`).URL
	rs, err := ddg.Search(ctx, "go", 3)
	if err != nil || len(rs) != 1 || rs[0].URL != "https://go.dev/" || rs[0].Title != "The Go site" || rs[0].Content != "Build simple software" {
		t.Fatalf("%+v %v", rs, err)
	}

	wk := &search.Wikipedia{}
	wk.URL = mk(`{"query":{"pages":{"1":{"title":"Go (language)","extract":"Go is...","index":2},"2":{"title":"Golang","extract":"alias","index":1}}}}`).URL
	rs, err = wk.Search(ctx, "go", 3)
	if err != nil || len(rs) != 2 || rs[0].Title != "Golang" || !strings.HasPrefix(rs[1].URL, "https://en.wikipedia.org/wiki/Go_") {
		t.Fatalf("%+v %v", rs, err)
	}

	ax := &search.ArXiv{}
	ax.URL = mk(`<feed><entry><title>Attention
  is all</title><id>http://arxiv.org/abs/1706.03762</id><summary>We propose  Transformer.</summary><author><name>A. Vaswani</name></author></entry></feed>`).URL
	rs, err = ax.Search(ctx, "attention", 1)
	if err != nil || rs[0].Title != "Attention is all" || !strings.Contains(rs[0].Content, "A. Vaswani") {
		t.Fatalf("%+v %v", rs, err)
	}

	hn := &search.HackerNews{}
	hn.URL = mk(`{"hits":[{"title":"Show HN","url":"","objectID":"7","points":10,"author":"pg"}]}`).URL
	rs, _ = hn.Search(ctx, "x", 1)
	if rs[0].URL != "https://news.ycombinator.com/item?id=7" {
		t.Fatal(rs)
	}

	bad := &search.Brave{}
	bad.URL = serve(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "quota", 429) }).URL
	if _, err := bad.Search(ctx, "q", 1); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatal(err)
	}
	tool := search.Tool("web_search", "Search.", tav, 3)
	out, err := tool.Call(ctx, map[string]any{"query": "x"})
	if err != nil || !strings.Contains(out.(string), "T1") {
		t.Fatal(out, err)
	}
}

func TestFetcherSSRFGuard(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "secret internal data") })
	ctx := context.Background()

	if _, err := (&search.Fetcher{}).Get(ctx, srv.URL); err == nil {
		t.Fatal("loopback must be blocked by default")
	}
	got, err := (&search.Fetcher{AllowPrivate: true}).Get(ctx, srv.URL)
	if err != nil || got != "secret internal data" {
		t.Fatal(got, err)
	}
	for _, u := range []string{"file:///etc/passwd", "ftp://x/y", "/relative", "http://"} {
		if _, err := (&search.Fetcher{AllowPrivate: true}).Get(ctx, u); err == nil {
			t.Fatalf("%q must be rejected", u)
		}
	}
	small := &search.Fetcher{AllowPrivate: true, MaxBytes: 6}
	if got, _ := small.Get(ctx, srv.URL); got != "secret" {
		t.Fatal(got)
	}
}

func TestFirecrawlAndJina(t *testing.T) {
	fc := &search.Firecrawl{}
	fc.URL, fc.APIKey = serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"markdown":"# Hi"}}`)
	}).URL, "k"
	if md, err := fc.Scrape(context.Background(), "https://x.dev"); err != nil || md != "# Hi" {
		t.Fatal(md, err)
	}
	jr := &search.JinaReader{}
	var path string
	jr.URL = serve(t, func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; fmt.Fprint(w, "page text") }).URL
	if txt, err := jr.Read(context.Background(), "https://x.dev/a"); err != nil || txt != "page text" || !strings.HasPrefix(path, "/https:") {
		t.Fatal(txt, err, path)
	}
}
