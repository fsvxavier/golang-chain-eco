// Package salesforce is a read-only Salesforce REST client: run SOQL queries
// and expose them to agents as a tool. Writes are deliberately not offered.
package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Client talks to one Salesforce org.
type Client struct {
	InstanceURL string // https://yourorg.my.salesforce.com
	AccessToken string // OAuth access token
	APIVersion  string // default v60.0
	HTTP        *http.Client
}

// Query runs a SOQL SELECT and returns up to max records (all pages followed
// until max is reached; max <= 0 means the first page only).
func (c *Client) Query(ctx context.Context, soql string, max int) ([]map[string]any, error) {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(soql)), "SELECT") {
		return nil, fmt.Errorf("salesforce: only SELECT queries are allowed")
	}
	ver := c.APIVersion
	if ver == "" {
		ver = "v60.0"
	}
	base := strings.TrimRight(c.InstanceURL, "/")
	next := "/services/data/" + ver + "/query?q=" + url.QueryEscape(soql)
	var records []map[string]any
	for next != "" {
		var page struct {
			Records        []map[string]any `json:"records"`
			Done           bool             `json:"done"`
			NextRecordsURL string           `json:"nextRecordsUrl"`
		}
		if err := c.get(ctx, base+next, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Records {
			delete(r, "attributes")
			records = append(records, r)
		}
		next = ""
		if !page.Done && page.NextRecordsURL != "" && max > 0 && len(records) < max {
			next = page.NextRecordsURL
		}
	}
	if max > 0 && len(records) > max {
		records = records[:max]
	}
	return records, nil
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("salesforce: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	return json.Unmarshal(raw, out)
}

type queryArgs struct {
	SOQL string `json:"soql" description:"A SOQL SELECT statement, e.g. SELECT Id, Name FROM Account LIMIT 5"`
}

// Tool returns the salesforce_query agent tool (read-only, at most maxRecords rows).
func (c *Client) Tool(maxRecords int) tools.Tool {
	if maxRecords <= 0 {
		maxRecords = 50
	}
	return tools.New("salesforce_query", "Run a read-only SOQL SELECT against Salesforce and return the records as JSON.",
		func(ctx context.Context, a queryArgs) (any, error) {
			recs, err := c.Query(ctx, a.SOQL, maxRecords)
			if err != nil {
				return nil, tools.Errorf("query failed: %v", err)
			}
			b, _ := json.Marshal(recs)
			return string(b), nil
		})
}
