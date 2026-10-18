// Package search wraps web/knowledge search APIs behind one Searcher
// interface, usable as an agent tool (Tool) or as a retriever (Retriever):
// Tavily, Exa, Brave, Serper, SerpAPI, SearXNG, DuckDuckGo, Wikipedia, arXiv
// and Hacker News.
package search

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Result is one search hit.
type Result struct {
	Title   string
	URL     string
	Content string
	Score   float64
}

// Searcher runs a query and returns up to max results (0 = provider default).
type Searcher interface {
	Search(ctx context.Context, query string, max int) ([]Result, error)
}

// Format renders results as text for an LLM.
func Format(rs []Result) string {
	if len(rs) == 0 {
		return "No results found."
	}
	var b strings.Builder
	for i, r := range rs {
		fmt.Fprintf(&b, "[%d] %s\n%s\n%s\n\n", i+1, r.Title, r.URL, strings.TrimSpace(r.Content))
	}
	return strings.TrimSpace(b.String())
}

type queryArgs struct {
	Query string `json:"query" description:"Search query"`
}

// Tool exposes s as an agent tool.
func Tool(name, description string, s Searcher, max int) tools.Tool {
	return tools.New(name, description, func(ctx context.Context, a queryArgs) (any, error) {
		rs, err := s.Search(ctx, a.Query, max)
		if err != nil {
			return nil, tools.Errorf("search failed: %v", err)
		}
		return Format(rs), nil
	})
}

// Retriever exposes s as a retriever; titles and URLs go to document metadata.
func Retriever(s Searcher, max int) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		rs, err := s.Search(ctx, q, max)
		if err != nil {
			return nil, err
		}
		docs := make([]documents.Document, len(rs))
		for i, r := range rs {
			docs[i] = documents.Document{PageContent: r.Content, Metadata: map[string]any{"title": r.Title, "source": r.URL, "score": r.Score}}
		}
		return docs, nil
	})
}

// ---------------------------------------------------------------- plumbing

type base struct {
	URL    string
	APIKey string
	HTTP   *http.Client
}

func (b base) key(env string) string {
	if b.APIKey != "" {
		return b.APIKey
	}
	return os.Getenv(env)
}

func (b base) url(dflt string) string {
	if b.URL != "" {
		return strings.TrimRight(b.URL, "/")
	}
	return dflt
}

const maxBody = 4 << 20

func (b base) do(ctx context.Context, method, u string, body any, headers map[string]string, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := b.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	if s, ok := out.(*string); ok {
		*s = string(raw)
		return nil
	}
	return json.Unmarshal(raw, out)
}

func orN(max, d int) int {
	if max > 0 {
		return max
	}
	return d
}

// ---------------------------------------------------------------- engines

// Tavily is the Tavily search API ($TAVILY_API_KEY).
type Tavily struct {
	base
	Depth         string // basic (default) or advanced
	IncludeAnswer bool
}

// NewTavily creates a Tavily searcher.
func NewTavily(apiKey string) *Tavily { return &Tavily{base: base{APIKey: apiKey}} }

func (t *Tavily) Search(ctx context.Context, q string, max int) ([]Result, error) {
	depth := t.Depth
	if depth == "" {
		depth = "basic"
	}
	var out struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title, URL, Content string
			Score               float64
		} `json:"results"`
	}
	err := t.do(ctx, "POST", t.url("https://api.tavily.com")+"/search",
		map[string]any{"query": q, "max_results": orN(max, 5), "search_depth": depth, "include_answer": t.IncludeAnswer},
		map[string]string{"Authorization": "Bearer " + t.key("TAVILY_API_KEY")}, &out)
	if err != nil {
		return nil, err
	}
	var rs []Result
	if out.Answer != "" {
		rs = append(rs, Result{Title: "Answer", Content: out.Answer, Score: 1})
	}
	for _, r := range out.Results {
		rs = append(rs, Result{r.Title, r.URL, r.Content, r.Score})
	}
	return rs, nil
}

// Exa is the Exa neural search API ($EXA_API_KEY).
type Exa struct{ base }

func (e *Exa) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Results []struct {
			Title, URL, Text string
			Score            float64
		} `json:"results"`
	}
	err := e.do(ctx, "POST", e.url("https://api.exa.ai")+"/search",
		map[string]any{"query": q, "numResults": orN(max, 5), "contents": map[string]any{"text": map[string]any{"maxCharacters": 2000}}},
		map[string]string{"x-api-key": e.key("EXA_API_KEY")}, &out)
	rs := make([]Result, len(out.Results))
	for i, r := range out.Results {
		rs[i] = Result{r.Title, r.URL, r.Text, r.Score}
	}
	return rs, err
}

// Brave is the Brave Search API ($BRAVE_API_KEY).
type Brave struct{ base }

func (b *Brave) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Web struct {
			Results []struct{ Title, URL, Description string } `json:"results"`
		} `json:"web"`
	}
	err := b.do(ctx, "GET", b.url("https://api.search.brave.com")+"/res/v1/web/search?q="+url.QueryEscape(q)+"&count="+strconv.Itoa(orN(max, 5)), nil,
		map[string]string{"X-Subscription-Token": b.key("BRAVE_API_KEY"), "Accept": "application/json"}, &out)
	rs := make([]Result, len(out.Web.Results))
	for i, r := range out.Web.Results {
		rs[i] = Result{Title: stripTags(r.Title), URL: r.URL, Content: stripTags(r.Description)}
	}
	return rs, err
}

// Serper is Google results via serper.dev ($SERPER_API_KEY).
type Serper struct{ base }

func (s *Serper) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		AnswerBox struct{ Answer, Snippet string }        `json:"answerBox"`
		Organic   []struct{ Title, Link, Snippet string } `json:"organic"`
	}
	err := s.do(ctx, "POST", s.url("https://google.serper.dev")+"/search", map[string]any{"q": q, "num": orN(max, 5)},
		map[string]string{"X-API-KEY": s.key("SERPER_API_KEY")}, &out)
	var rs []Result
	if a := firstNonEmpty(out.AnswerBox.Answer, out.AnswerBox.Snippet); a != "" {
		rs = append(rs, Result{Title: "Answer box", Content: a, Score: 1})
	}
	for _, r := range out.Organic {
		rs = append(rs, Result{Title: r.Title, URL: r.Link, Content: r.Snippet})
	}
	return rs, err
}

// SerpAPI is Google (or another engine) via serpapi.com ($SERPAPI_API_KEY).
type SerpAPI struct {
	base
	Engine string // default google
}

func (s *SerpAPI) Search(ctx context.Context, q string, max int) ([]Result, error) {
	eng := s.Engine
	if eng == "" {
		eng = "google"
	}
	var out struct {
		Organic []struct{ Title, Link, Snippet string } `json:"organic_results"`
	}
	u := s.url("https://serpapi.com") + "/search.json?engine=" + eng + "&q=" + url.QueryEscape(q) + "&num=" + strconv.Itoa(orN(max, 5)) + "&api_key=" + url.QueryEscape(s.key("SERPAPI_API_KEY"))
	err := s.do(ctx, "GET", u, nil, nil, &out)
	rs := make([]Result, len(out.Organic))
	for i, r := range out.Organic {
		rs[i] = Result{Title: r.Title, URL: r.Link, Content: r.Snippet}
	}
	return rs, err
}

// SearXNG queries a self-hosted SearXNG instance (URL required).
type SearXNG struct{ base }

func (s *SearXNG) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Results []struct {
			Title, URL, Content string
			Score               float64
		} `json:"results"`
	}
	err := s.do(ctx, "GET", s.url("http://localhost:8080")+"/search?format=json&q="+url.QueryEscape(q), nil, nil, &out)
	n := min(orN(max, 5), len(out.Results))
	rs := make([]Result, n)
	for i := 0; i < n; i++ {
		r := out.Results[i]
		rs[i] = Result{r.Title, r.URL, r.Content, r.Score}
	}
	return rs, err
}

var (
	ddgLink    = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippet = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
)

// DuckDuckGo scrapes the HTML endpoint (no key). It depends on page markup
// and may break or be rate-limited; prefer an API-backed engine in production.
type DuckDuckGo struct{ base }

func (d *DuckDuckGo) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var page string
	err := d.do(ctx, "GET", d.url("https://html.duckduckgo.com")+"/html/?q="+url.QueryEscape(q), nil,
		map[string]string{"User-Agent": "Mozilla/5.0 (compatible; golang-chain-eco)"}, &page)
	if err != nil {
		return nil, err
	}
	links, snips := ddgLink.FindAllStringSubmatch(page, -1), ddgSnippet.FindAllStringSubmatch(page, -1)
	var rs []Result
	for i, l := range links {
		if len(rs) >= orN(max, 5) {
			break
		}
		r := Result{Title: stripTags(l[2]), URL: ddgTarget(l[1])}
		if i < len(snips) {
			r.Content = stripTags(snips[i][1])
		}
		rs = append(rs, r)
	}
	return rs, nil
}

// ddgTarget unwraps DuckDuckGo's redirect links (//duckduckgo.com/l/?uddg=...).
func ddgTarget(href string) string {
	href = html.UnescapeString(href)
	if u, err := url.Parse(href); err == nil {
		if t := u.Query().Get("uddg"); t != "" {
			return t
		}
	}
	return href
}

// Wikipedia searches article intros through the MediaWiki API.
type Wikipedia struct {
	base
	Lang string // default en
}

func (w *Wikipedia) Search(ctx context.Context, q string, max int) ([]Result, error) {
	lang := w.Lang
	if lang == "" {
		lang = "en"
	}
	var out struct {
		Query struct {
			Pages map[string]struct {
				Title   string
				Extract string
				Index   int
			} `json:"pages"`
		} `json:"query"`
	}
	u := w.url("https://"+lang+".wikipedia.org") + "/w/api.php?action=query&format=json&generator=search&prop=extracts&exintro=1&explaintext=1&exlimit=max&gsrlimit=" +
		strconv.Itoa(orN(max, 3)) + "&gsrsearch=" + url.QueryEscape(q)
	if err := w.do(ctx, "GET", u, nil, map[string]string{"User-Agent": "golang-chain-eco/0.1"}, &out); err != nil {
		return nil, err
	}
	rs := make([]Result, 0, len(out.Query.Pages))
	for _, p := range out.Query.Pages {
		rs = append(rs, Result{Title: p.Title, URL: "https://" + lang + ".wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(p.Title, " ", "_")),
			Content: p.Extract, Score: float64(100 - p.Index)})
	}
	sortByScore(rs)
	return rs, nil
}

// ArXiv searches arXiv preprints (Atom API).
type ArXiv struct{ base }

func (a *ArXiv) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var raw string
	err := a.do(ctx, "GET", a.url("https://export.arxiv.org")+"/api/query?search_query=all:"+url.QueryEscape(q)+"&max_results="+strconv.Itoa(orN(max, 3)), nil, nil, &raw)
	if err != nil {
		return nil, err
	}
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			ID      string `xml:"id"`
			Summary string `xml:"summary"`
			Authors []struct {
				Name string `xml:"name"`
			} `xml:"author"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(raw), &feed); err != nil {
		return nil, err
	}
	rs := make([]Result, len(feed.Entries))
	for i, e := range feed.Entries {
		var names []string
		for _, au := range e.Authors {
			names = append(names, au.Name)
		}
		rs[i] = Result{Title: oneLine(e.Title), URL: strings.TrimSpace(e.ID),
			Content: "Authors: " + strings.Join(names, ", ") + "\n" + oneLine(e.Summary)}
	}
	return rs, nil
}

// HackerNews searches stories through the Algolia API.
type HackerNews struct{ base }

func (h *HackerNews) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Hits []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			ObjectID string `json:"objectID"`
			Points   int    `json:"points"`
			Author   string `json:"author"`
		} `json:"hits"`
	}
	err := h.do(ctx, "GET", h.url("https://hn.algolia.com")+"/api/v1/search?tags=story&hitsPerPage="+strconv.Itoa(orN(max, 5))+"&query="+url.QueryEscape(q), nil, nil, &out)
	rs := make([]Result, len(out.Hits))
	for i, x := range out.Hits {
		u := x.URL
		if u == "" {
			u = "https://news.ycombinator.com/item?id=" + x.ObjectID
		}
		rs[i] = Result{Title: x.Title, URL: u, Content: fmt.Sprintf("%d points by %s", x.Points, x.Author), Score: float64(x.Points)}
	}
	return rs, err
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(s, "")))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func sortByScore(rs []Result) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].Score > rs[j-1].Score; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}
