package search

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Firecrawl scrapes a page to markdown ($FIRECRAWL_API_KEY).
type Firecrawl struct{ base }

// Scrape returns the page as markdown.
func (f *Firecrawl) Scrape(ctx context.Context, pageURL string) (string, error) {
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Markdown string `json:"markdown"`
		} `json:"data"`
		Error string `json:"error"`
	}
	err := f.do(ctx, "POST", f.url("https://api.firecrawl.dev")+"/v1/scrape", map[string]any{"url": pageURL, "formats": []string{"markdown"}},
		map[string]string{"Authorization": "Bearer " + f.key("FIRECRAWL_API_KEY")}, &out)
	if err == nil && !out.Success {
		err = errors.New(firstNonEmpty(out.Error, "scrape failed"))
	}
	return out.Data.Markdown, err
}

// JinaReader converts any URL to LLM-friendly text through r.jina.ai
// ($JINA_API_KEY is optional).
type JinaReader struct{ base }

// Read fetches pageURL.
func (j *JinaReader) Read(ctx context.Context, pageURL string) (string, error) {
	h := map[string]string{}
	if k := j.key("JINA_API_KEY"); k != "" {
		h["Authorization"] = "Bearer " + k
	}
	var text string
	err := j.do(ctx, "GET", j.url("https://r.jina.ai")+"/"+pageURL, nil, h, &text)
	return text, err
}

// Fetcher downloads http(s) pages for agents (RequestsGetTool). By default it
// refuses loopback, private and link-local addresses so a prompt-injected
// agent cannot reach internal services (SSRF); set AllowPrivate to opt out.
type Fetcher struct {
	AllowPrivate bool
	MaxBytes     int64 // default 200 KB
	HTTP         *http.Client
}

func (f *Fetcher) client() *http.Client {
	if f.AllowPrivate {
		if f.HTTP != nil {
			return f.HTTP
		}
		return http.DefaultClient
	}
	dialer := &net.Dialer{}
	// validate the resolved IP at connect time (defeats DNS rebinding)
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()) {
			return fmt.Errorf("blocked address %s", ip)
		}
		return nil
	}
	return &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
}

// Get returns the response body as text, truncated to MaxBytes.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("only absolute http(s) URLs are allowed: %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	limit := f.MaxBytes
	if limit <= 0 {
		limit = 200 << 10
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	return strings.TrimSpace(string(b)), nil
}

type urlArgs struct {
	URL string `json:"url" description:"Absolute http(s) URL"`
}

// Tool exposes the fetcher as "fetch_url".
func (f *Fetcher) Tool() tools.Tool {
	return tools.New("fetch_url", "Download a web page or API response (GET) and return its text.",
		func(ctx context.Context, a urlArgs) (any, error) {
			s, err := f.Get(ctx, a.URL)
			if err != nil {
				return nil, tools.Errorf("fetch failed: %v", err)
			}
			return s, nil
		})
}

// ScrapeTool exposes any "URL to text" function (Firecrawl.Scrape,
// JinaReader.Read) as an agent tool.
func ScrapeTool(name, description string, fn func(context.Context, string) (string, error)) tools.Tool {
	return tools.New(name, description, func(ctx context.Context, a urlArgs) (any, error) {
		s, err := fn(ctx, a.URL)
		if err != nil {
			return nil, tools.Errorf("%s failed: %v", name, err)
		}
		return s, nil
	})
}
