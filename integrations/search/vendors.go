package search

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Contracts below follow each vendor's public API documentation; they are
// covered by httptest-based tests, not by calls to the live services.

// Zotero searches a Zotero library ($ZOTERO_API_KEY). Library is
// "users/<id>" or "groups/<id>".
type Zotero struct {
	base
	Library string
}

func (z *Zotero) Search(ctx context.Context, q string, max int) ([]Result, error) {
	if z.Library == "" {
		return nil, fmt.Errorf("zotero: Library is required (users/<id> or groups/<id>)")
	}
	var items []struct {
		Key  string
		Data struct {
			Title        string
			AbstractNote string
			URL          string
			DOI          string
			Date         string
			Creators     []struct{ FirstName, LastName, Name string }
		}
	}
	err := z.do(ctx, "GET", z.url("https://api.zotero.org")+"/"+z.Library+"/items?format=json&qmode=everything&limit="+strconv.Itoa(orN(max, 5))+"&q="+url.QueryEscape(q), nil,
		map[string]string{"Zotero-API-Key": z.key("ZOTERO_API_KEY"), "Zotero-API-Version": "3"}, &items)
	var rs []Result
	for _, it := range items {
		if it.Data.Title == "" { // attachments and notes have no title
			continue
		}
		var by []string
		for _, c := range it.Data.Creators {
			by = append(by, firstNonEmpty(c.Name, strings.TrimSpace(c.FirstName+" "+c.LastName)))
		}
		link := firstNonEmpty(it.Data.URL, func() string {
			if it.Data.DOI != "" {
				return "https://doi.org/" + it.Data.DOI
			}
			return ""
		}())
		rs = append(rs, Result{Title: it.Data.Title, URL: link,
			Content: strings.TrimSpace(strings.Join(by, ", ") + " (" + it.Data.Date + "). " + it.Data.AbstractNote)})
	}
	return rs, err
}

// YouTube searches videos with the YouTube Data API v3 ($YOUTUBE_API_KEY).
type YouTube struct{ base }

func (y *YouTube) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			}
			Snippet struct {
				Title, Description, ChannelTitle, PublishedAt string
			}
		}
	}
	err := y.do(ctx, "GET", y.url("https://www.googleapis.com")+"/youtube/v3/search?part=snippet&type=video&maxResults="+strconv.Itoa(orN(max, 5))+
		"&q="+url.QueryEscape(q)+"&key="+url.QueryEscape(y.key("YOUTUBE_API_KEY")), nil, nil, &out)
	rs := make([]Result, len(out.Items))
	for i, it := range out.Items {
		rs[i] = Result{Title: it.Snippet.Title, URL: "https://www.youtube.com/watch?v=" + it.ID.VideoID,
			Content: it.Snippet.ChannelTitle + " (" + it.Snippet.PublishedAt + "): " + it.Snippet.Description}
	}
	return rs, err
}

// Perigon searches news with the Perigon API ($PERIGON_API_KEY).
type Perigon struct{ base }

func (p *Perigon) Search(ctx context.Context, q string, max int) ([]Result, error) {
	var out struct {
		Articles []struct{ Title, URL, Description, Summary, Content string }
	}
	err := p.do(ctx, "GET", p.url("https://api.goperigon.com")+"/v1/all?size="+strconv.Itoa(orN(max, 5))+"&sortBy=relevance&q="+url.QueryEscape(q)+
		"&apiKey="+url.QueryEscape(p.key("PERIGON_API_KEY")), nil, nil, &out)
	rs := make([]Result, len(out.Articles))
	for i, a := range out.Articles {
		rs[i] = Result{Title: a.Title, URL: a.URL, Content: firstNonEmpty(a.Summary, a.Description, a.Content)}
	}
	return rs, err
}

// Oxylabs runs Google searches through the Web Scraper Realtime API.
// APIKey is "username:password" ($OXYLABS_CREDENTIALS).
type Oxylabs struct{ base }

func (o *Oxylabs) Search(ctx context.Context, q string, max int) ([]Result, error) {
	cred := o.key("OXYLABS_CREDENTIALS")
	var out struct {
		Results []struct {
			Content struct {
				Results struct {
					Organic []struct{ Title, URL, Desc string }
				}
			}
		}
	}
	err := o.do(ctx, "POST", o.url("https://realtime.oxylabs.io")+"/v1/queries", map[string]any{"source": "google_search", "query": q, "parse": true},
		map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))}, &out)
	var rs []Result
	for _, r := range out.Results {
		for _, h := range r.Content.Results.Organic {
			rs = append(rs, Result{Title: h.Title, URL: h.URL, Content: h.Desc})
			if max > 0 && len(rs) >= max {
				return rs, err
			}
		}
	}
	return rs, err
}
