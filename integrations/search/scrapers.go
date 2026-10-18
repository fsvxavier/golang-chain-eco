package search

import (
	"context"
	"errors"
	"net/url"
)

// The scrapers below share Firecrawl.Scrape's signature, so each plugs into
// ScrapeTool. ScraperAPI, ScrapingBee and Brightdata return the page as
// fetched (HTML; pass it through loaders.HTMLToText for plain text).

// ScraperAPI fetches pages through scraperapi.com ($SCRAPERAPI_API_KEY).
type ScraperAPI struct{ base }

func (s *ScraperAPI) Scrape(ctx context.Context, pageURL string) (string, error) {
	var out string
	err := s.do(ctx, "GET", s.url("https://api.scraperapi.com")+"/?api_key="+url.QueryEscape(s.key("SCRAPERAPI_API_KEY"))+"&url="+url.QueryEscape(pageURL), nil, nil, &out)
	return out, err
}

// ScrapingBee fetches pages through scrapingbee.com ($SCRAPINGBEE_API_KEY).
type ScrapingBee struct{ base }

func (s *ScrapingBee) Scrape(ctx context.Context, pageURL string) (string, error) {
	var out string
	err := s.do(ctx, "GET", s.url("https://app.scrapingbee.com")+"/api/v1/?api_key="+url.QueryEscape(s.key("SCRAPINGBEE_API_KEY"))+"&url="+url.QueryEscape(pageURL), nil, nil, &out)
	return out, err
}

// Brightdata fetches pages through the Web Unlocker API ($BRIGHTDATA_API_KEY;
// Zone is the Unlocker zone name).
type Brightdata struct {
	base
	Zone string
}

func (b *Brightdata) Scrape(ctx context.Context, pageURL string) (string, error) {
	if b.Zone == "" {
		return "", errors.New("brightdata: Zone is required")
	}
	var out string
	err := b.do(ctx, "POST", b.url("https://api.brightdata.com")+"/request", map[string]any{"zone": b.Zone, "url": pageURL, "format": "raw"},
		map[string]string{"Authorization": "Bearer " + b.key("BRIGHTDATA_API_KEY")}, &out)
	return out, err
}

// Diffbot extracts the main article text via the Article API ($DIFFBOT_TOKEN).
type Diffbot struct{ base }

func (d *Diffbot) Scrape(ctx context.Context, pageURL string) (string, error) {
	var out struct {
		Objects []struct{ Title, Text string } `json:"objects"`
		Error   string                         `json:"error"`
	}
	err := d.do(ctx, "GET", d.url("https://api.diffbot.com")+"/v3/article?token="+url.QueryEscape(d.key("DIFFBOT_TOKEN"))+"&url="+url.QueryEscape(pageURL), nil, nil, &out)
	if err != nil {
		return "", err
	}
	if len(out.Objects) == 0 {
		return "", errors.New("diffbot: " + firstNonEmpty(out.Error, "no article extracted"))
	}
	return out.Objects[0].Title + "\n\n" + out.Objects[0].Text, nil
}
