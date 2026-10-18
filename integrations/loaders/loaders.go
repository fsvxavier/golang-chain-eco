// Package loaders adds document loaders beyond core/documents (Text, CSV,
// JSONLines, HTML, Directory): web pages, sitemaps, JSON files, and the
// Unstructured API for PDF/Office/e-mail/image formats.
package loaders

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/integrations/search"
)

var (
	scriptRes = func() (rs []*regexp.Regexp) { // RE2 has no backreferences: one pattern per tag
		for _, t := range []string{"script", "style", "noscript", "svg", "head", "title"} {
			rs = append(rs, regexp.MustCompile(`(?is)<`+t+`[^>]*>.*?</`+t+`>`))
		}
		return
	}()
	breakRe = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|br)\s*>|<br\s*/?>`)
	tagRe   = regexp.MustCompile(`(?s)<[^>]+>`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	blankRe = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText strips markup, scripts and styles, keeping paragraph breaks.
func HTMLToText(page string) (title, text string) {
	if m := titleRe.FindStringSubmatch(page); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	for _, re := range scriptRes {
		page = re.ReplaceAllString(page, "")
	}
	page = breakRe.ReplaceAllString(page, "\n")
	text = html.UnescapeString(tagRe.ReplaceAllString(page, ""))
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return title, strings.TrimSpace(blankRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// Web loads URLs as text documents (WebBaseLoader). Fetching goes through
// search.Fetcher, so private/loopback addresses are blocked unless
// Fetcher.AllowPrivate is set.
func Web(f *search.Fetcher, urls ...string) documents.Loader {
	return documents.LoaderFunc(func(ctx context.Context) ([]documents.Document, error) {
		var docs []documents.Document
		for _, u := range urls {
			page, err := f.Get(ctx, u)
			if err != nil {
				return nil, fmt.Errorf("loaders: %s: %w", u, err)
			}
			title, text := HTMLToText(page)
			docs = append(docs, documents.Document{PageContent: text, Metadata: map[string]any{"source": u, "title": title}})
		}
		return docs, nil
	})
}

// Sitemap returns the page URLs listed in a sitemap.xml (follows nested
// sitemap indexes one level deep), optionally filtered by a substring.
func Sitemap(ctx context.Context, f *search.Fetcher, sitemapURL, contains string) ([]string, error) {
	type set struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	fetch := func(u string) (set, error) {
		var s set
		raw, err := f.Get(ctx, u)
		if err != nil {
			return s, err
		}
		return s, xml.Unmarshal([]byte(raw), &s)
	}
	top, err := fetch(sitemapURL)
	if err != nil {
		return nil, err
	}
	var out []string
	add := func(s set) {
		for _, u := range s.URLs {
			if loc := strings.TrimSpace(u.Loc); loc != "" && strings.Contains(loc, contains) {
				out = append(out, loc)
			}
		}
	}
	add(top)
	for _, sm := range top.Sitemaps {
		sub, err := fetch(strings.TrimSpace(sm.Loc))
		if err != nil {
			return nil, err
		}
		add(sub)
	}
	return out, nil
}

// JSON loads a JSON file: one document per element of the array found at
// path (dot-separated keys; "" for a top-level array). contentKey selects
// the text field; the remaining fields become metadata.
func JSON(file, path, contentKey string) documents.Loader {
	return documents.LoaderFunc(func(context.Context) ([]documents.Document, error) {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var root any
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
		for _, k := range strings.Split(path, ".") {
			if k == "" {
				continue
			}
			m, ok := root.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("loaders: path %q: %T is not an object", path, root)
			}
			root = m[k]
		}
		items, ok := root.([]any)
		if !ok {
			return nil, fmt.Errorf("loaders: %q is not an array", path)
		}
		docs := make([]documents.Document, 0, len(items))
		for i, it := range items {
			d := documents.Document{Metadata: map[string]any{"source": file, "seq_num": i + 1}}
			if m, ok := it.(map[string]any); ok {
				for k, v := range m {
					if k == contentKey {
						d.PageContent = fmt.Sprint(v)
					} else {
						d.Metadata[k] = v
					}
				}
			} else {
				d.PageContent = fmt.Sprint(it)
			}
			docs = append(docs, d)
		}
		return docs, nil
	})
}

// Unstructured parses PDFs, Office files, e-mails and images through the
// Unstructured API ($UNSTRUCTURED_API_KEY; point URL at a self-hosted
// instance to keep data on-premises).
type Unstructured struct {
	URL      string // default https://api.unstructured.io/general/v0/general
	APIKey   string
	Strategy string // auto (default), fast, hi_res, ocr_only
	// Single merges all elements into one document per file.
	Single bool
	HTTP   *http.Client
}

// File loads one local file.
func (u *Unstructured) File(path string) documents.Loader {
	return documents.LoaderFunc(func(ctx context.Context) ([]documents.Document, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return u.parse(ctx, filepath.Base(path), f)
	})
}

func (u *Unstructured) parse(ctx context.Context, name string, r io.Reader) ([]documents.Document, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	strategy := u.Strategy
	if strategy == "" {
		strategy = "auto"
	}
	_ = mw.WriteField("strategy", strategy)
	fw, err := mw.CreateFormFile("files", name)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, r); err != nil {
		return nil, err
	}
	_ = mw.Close()
	endpoint := u.URL
	if endpoint == "" {
		endpoint = "https://api.unstructured.io/general/v0/general"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	key := u.APIKey
	if key == "" {
		key = os.Getenv("UNSTRUCTURED_API_KEY")
	}
	if key != "" {
		req.Header.Set("unstructured-api-key", key)
	}
	hc := u.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unstructured: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var els []struct {
		Type     string         `json:"type"`
		Text     string         `json:"text"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &els); err != nil {
		return nil, fmt.Errorf("unstructured: bad response: %w", err)
	}
	var docs []documents.Document
	var all []string
	for _, e := range els {
		if strings.TrimSpace(e.Text) == "" {
			continue
		}
		if u.Single {
			all = append(all, e.Text)
			continue
		}
		md := map[string]any{"source": name, "category": e.Type}
		for k, v := range e.Metadata {
			md[k] = v
		}
		docs = append(docs, documents.Document{PageContent: e.Text, Metadata: md})
	}
	if u.Single {
		docs = []documents.Document{{PageContent: strings.Join(all, "\n\n"), Metadata: map[string]any{"source": name}}}
	}
	return docs, nil
}
