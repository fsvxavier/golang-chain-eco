package search

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// YouCom is the You.com search API ($YDC_API_KEY).
type YouCom struct{ base }

func (y *YouCom) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Hits []struct {
			Title, URL, Description string
			Snippets                []string
		} `json:"hits"`
	}
	err := y.do(ctx, "GET", y.url("https://api.ydc-index.io")+"/search?query="+url.QueryEscape(q), nil,
		map[string]string{"X-API-Key": y.key("YDC_API_KEY")}, &out)
	var rs []Result
	for _, h := range out.Hits {
		rs = append(rs, Result{Title: h.Title, URL: h.URL, Content: firstNonEmpty(strings.Join(h.Snippets, " "), h.Description)})
		if max > 0 && len(rs) >= max {
			break
		}
	}
	return rs, err
}

// Linkup is the Linkup search API ($LINKUP_API_KEY).
type Linkup struct{ base }

func (l *Linkup) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Results []struct{ Name, URL, Content string } `json:"results"`
	}
	err := l.do(ctx, "POST", l.url("https://api.linkup.so")+"/v1/search",
		map[string]any{"q": q, "depth": "standard", "outputType": "searchResults"},
		map[string]string{"Authorization": "Bearer " + l.key("LINKUP_API_KEY")}, &out)
	var rs []Result
	for _, r := range out.Results {
		rs = append(rs, Result{Title: r.Name, URL: r.URL, Content: r.Content})
		if max > 0 && len(rs) >= max {
			break
		}
	}
	return rs, err
}

// PerplexitySearch is the Perplexity Search API ($PERPLEXITY_API_KEY).
type PerplexitySearch struct{ base }

func (p *PerplexitySearch) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Results []struct{ Title, URL, Snippet string } `json:"results"`
	}
	err := p.do(ctx, "POST", p.url("https://api.perplexity.ai")+"/search",
		map[string]any{"query": q, "max_results": orN(max, 5)},
		map[string]string{"Authorization": "Bearer " + p.key("PERPLEXITY_API_KEY")}, &out)
	rs := make([]Result, len(out.Results))
	for i, r := range out.Results {
		rs[i] = Result{Title: r.Title, URL: r.URL, Content: r.Snippet}
	}
	return rs, err
}

// SearchAPI is SearchApi.io Google results ($SEARCHAPI_API_KEY).
type SearchAPI struct{ base }

func (s *SearchAPI) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Organic []struct{ Title, Link, Snippet string } `json:"organic_results"`
	}
	err := s.do(ctx, "GET", s.url("https://www.searchapi.io")+"/api/v1/search?engine=google&q="+url.QueryEscape(q), nil,
		map[string]string{"Authorization": "Bearer " + s.key("SEARCHAPI_API_KEY")}, &out)
	var rs []Result
	for _, r := range out.Organic {
		rs = append(rs, Result{Title: r.Title, URL: r.Link, Content: r.Snippet})
		if max > 0 && len(rs) >= max {
			break
		}
	}
	return rs, err
}

// JinaSearch is s.jina.ai ($JINA_API_KEY).
type JinaSearch struct{ base }

func (j *JinaSearch) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Data []struct{ Title, URL, Description, Content string } `json:"data"`
	}
	err := j.do(ctx, "GET", j.url("https://s.jina.ai")+"/?q="+url.QueryEscape(q), nil,
		map[string]string{"Accept": "application/json", "Authorization": "Bearer " + j.key("JINA_API_KEY")}, &out)
	var rs []Result
	for _, r := range out.Data {
		rs = append(rs, Result{Title: r.Title, URL: r.URL, Content: firstNonEmpty(r.Description, r.Content)})
		if max > 0 && len(rs) >= max {
			break
		}
	}
	return rs, err
}

// StackOverflow searches Stack Exchange (default site "stackoverflow"; no key
// needed, $STACKEXCHANGE_KEY raises the quota).
type StackOverflow struct {
	base
	Site string
}

func (s *StackOverflow) Search(ctx context.Context, q string, max int) ([]Result, error) {
	site := firstNonEmpty(s.Site, "stackoverflow")
	var out struct {
		Items []struct {
			Title, Link string
			Score       float64
			Tags        []string
		} `json:"items"`
	}
	u := s.url("https://api.stackexchange.com") + "/2.3/search/advanced?order=desc&sort=relevance&site=" + url.QueryEscape(site) +
		"&pagesize=" + strconv.Itoa(orN(max, 5)) + "&q=" + url.QueryEscape(q)
	if k := s.key("STACKEXCHANGE_KEY"); k != "" {
		u += "&key=" + url.QueryEscape(k)
	}
	err := s.do(ctx, "GET", u, nil, nil, &out)
	rs := make([]Result, len(out.Items))
	for i, it := range out.Items {
		rs[i] = Result{Title: stripTags(it.Title), URL: it.Link, Content: "tags: " + strings.Join(it.Tags, ", "), Score: it.Score}
	}
	return rs, err
}

// Reddit searches posts through the public JSON endpoint.
type Reddit struct{ base }

func (r *Reddit) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Data struct {
			Children []struct {
				Data struct {
					Title, Permalink, Selftext, Subreddit string
					Score                                 float64
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	err := r.do(ctx, "GET", r.url("https://www.reddit.com")+"/search.json?limit="+strconv.Itoa(orN(max, 5))+"&q="+url.QueryEscape(q), nil,
		map[string]string{"User-Agent": "golang-chain-eco/1.0"}, &out)
	var rs []Result
	for _, c := range out.Data.Children {
		d := c.Data
		rs = append(rs, Result{Title: d.Title, URL: "https://www.reddit.com" + d.Permalink,
			Content: "r/" + d.Subreddit + ": " + oneLine(d.Selftext), Score: d.Score})
	}
	return rs, err
}

// OpenAlex searches scholarly works (no key; $OPENALEX_MAILTO joins the polite pool).
type OpenAlex struct{ base }

func (o *OpenAlex) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Results []struct {
			DisplayName string `json:"display_name"`
			ID          string
			DOI         string
			Year        int `json:"publication_year"`
			Cited       int `json:"cited_by_count"`
		} `json:"results"`
	}
	u := o.url("https://api.openalex.org") + "/works?per-page=" + strconv.Itoa(orN(max, 5)) + "&search=" + url.QueryEscape(q)
	if m := o.key("OPENALEX_MAILTO"); m != "" {
		u += "&mailto=" + url.QueryEscape(m)
	}
	err := o.do(ctx, "GET", u, nil, nil, &out)
	rs := make([]Result, len(out.Results))
	for i, r := range out.Results {
		rs[i] = Result{Title: r.DisplayName, URL: firstNonEmpty(r.DOI, r.ID), Content: strconv.Itoa(r.Year) + ", cited by " + strconv.Itoa(r.Cited)}
	}
	return rs, err
}

// Apify runs an Apify Actor synchronously and returns its dataset items
// ($APIFY_API_TOKEN). The query is passed as the actor input under InputKey
// (default "query"); Input adds fixed fields.
type Apify struct {
	base
	Actor    string // e.g. "apify/google-search-scraper"
	InputKey string
	Input    map[string]any
}

func (a *Apify) Search(ctx context.Context, q string, max int) ([]Result, error) {
	in := map[string]any{firstNonEmpty(a.InputKey, "query"): q}
	for k, v := range a.Input {
		in[k] = v
	}
	var items []map[string]any
	err := a.do(ctx, "POST", a.url("https://api.apify.com")+"/v2/acts/"+strings.ReplaceAll(a.Actor, "/", "~")+
		"/run-sync-get-dataset-items?token="+url.QueryEscape(a.key("APIFY_API_TOKEN")), in, nil, &items)
	var rs []Result
	for _, it := range items {
		title, _ := it["title"].(string)
		u, _ := it["url"].(string)
		body, _ := it["text"].(string)
		if body == "" {
			body, _ = it["description"].(string)
		}
		rs = append(rs, Result{Title: title, URL: u, Content: body})
		if max > 0 && len(rs) >= max {
			break
		}
	}
	return rs, err
}
