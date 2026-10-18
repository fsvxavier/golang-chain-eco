// Package jenkins is a small Jenkins REST client (trigger builds, read build
// status and console output) with ready-made agent tools.
//
//	c := &jenkins.Client{URL: "https://ci.example.com", User: "bot", Token: "api-token"}
//	agentTools := c.Tools()
package jenkins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Client talks to a Jenkins controller.
type Client struct {
	URL   string
	User  string
	Token string // API token (or password)
	// HTTP must keep cookies: Jenkins ties its CSRF crumb to the web session.
	// Left nil, a cookie-keeping client is created.
	HTTP *http.Client

	once sync.Once
}

func (c *Client) client() *http.Client {
	c.once.Do(func() {
		if c.HTTP == nil {
			jar, _ := cookiejar.New(nil)
			c.HTTP = &http.Client{Jar: jar}
		}
	})
	return c.HTTP
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, ctype string) (*http.Response, error) {
	u := strings.TrimRight(c.URL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	hc := c.client()
	send := func(crumbField, crumb string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if c.User != "" {
			req.SetBasicAuth(c.User, c.Token)
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if crumb != "" {
			req.Header.Set(crumbField, crumb)
		}
		return hc.Do(req)
	}
	var field, crumb string
	if method != http.MethodGet { // CSRF protection applies to writes only
		field, crumb = c.crumb(ctx, hc)
	}
	resp, err := send(field, crumb)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("jenkins: %s %s: http %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return resp, nil
}

// crumb fetches the CSRF token; ("", "") when CSRF protection is off.
func (c *Client) crumb(ctx context.Context, hc *http.Client) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/crumbIssuer/api/json", nil)
	if err != nil {
		return "", ""
	}
	if c.User != "" {
		req.SetBasicAuth(c.User, c.Token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	var x struct{ Crumb, CrumbRequestField string }
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&x) != nil {
		return "", ""
	}
	return x.CrumbRequestField, x.Crumb
}

// jobPath turns "folder/job" into /job/folder/job/job.
func jobPath(job string) string {
	var b strings.Builder
	for _, p := range strings.Split(strings.Trim(job, "/"), "/") {
		b.WriteString("/job/" + url.PathEscape(p))
	}
	return b.String()
}

// Trigger queues a build, passing params when the job is parameterised, and
// returns the queue item URL.
func (c *Client) Trigger(ctx context.Context, job string, params map[string]string) (string, error) {
	path, q := jobPath(job)+"/build", url.Values(nil)
	if len(params) > 0 {
		path, q = jobPath(job)+"/buildWithParameters", url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
	}
	resp, err := c.do(ctx, http.MethodPost, path, q, nil, "")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	return resp.Header.Get("Location"), nil
}

// Build describes one run.
type Build struct {
	Number   int    `json:"number"`
	Result   string `json:"result"` // SUCCESS, FAILURE, ABORTED, UNSTABLE; "" while running
	Building bool   `json:"building"`
	URL      string `json:"url"`
}

// LastBuild returns the most recent build of job; ok=false if it never ran.
func (c *Client) LastBuild(ctx context.Context, job string) (b Build, ok bool, err error) {
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/lastBuild/api/json", nil, nil, "")
	if err != nil {
		if strings.Contains(err.Error(), "http 404") {
			return Build{}, false, nil
		}
		return Build{}, false, err
	}
	defer resp.Body.Close()
	return b, true, json.NewDecoder(resp.Body).Decode(&b)
}

// Console returns the last maxBytes of a build's console log.
func (c *Client) Console(ctx context.Context, job string, number, maxBytes int) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/"+strconv.Itoa(number)+"/consoleText", nil, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if maxBytes > 0 && len(raw) > maxBytes {
		raw = raw[len(raw)-maxBytes:]
	}
	return string(raw), err
}

// CreateJob creates a job from its config.xml.
func (c *Client) CreateJob(ctx context.Context, name, configXML string) error {
	resp, err := c.do(ctx, http.MethodPost, "/createItem", url.Values{"name": {name}}, []byte(configXML), "application/xml")
	if err == nil {
		resp.Body.Close()
	}
	return err
}

type triggerArgs struct {
	Job    string            `json:"job" description:"Job name, e.g. folder/my-job"`
	Params map[string]string `json:"params,omitempty" description:"Build parameters (optional)"`
}

type jobArgs struct {
	Job string `json:"job" description:"Job name, e.g. folder/my-job"`
}

// Tools returns agent tools: jenkins_trigger_build, jenkins_last_build and
// jenkins_console (tail of the last build's log).
func (c *Client) Tools() []tools.Tool {
	return []tools.Tool{
		tools.New("jenkins_trigger_build", "Queue a Jenkins build for a job.", func(ctx context.Context, a triggerArgs) (any, error) {
			loc, err := c.Trigger(ctx, a.Job, a.Params)
			if err != nil {
				return nil, tools.Errorf("trigger failed: %v", err)
			}
			return "queued: " + loc, nil
		}),
		tools.New("jenkins_last_build", "Get the status of the latest build of a Jenkins job.", func(ctx context.Context, a jobArgs) (any, error) {
			b, ok, err := c.LastBuild(ctx, a.Job)
			if err != nil {
				return nil, tools.Errorf("status failed: %v", err)
			}
			if !ok {
				return "job has no builds yet", nil
			}
			state := b.Result
			if b.Building {
				state = "RUNNING"
			}
			return fmt.Sprintf("build #%d: %s (%s)", b.Number, state, b.URL), nil
		}),
		tools.New("jenkins_console", "Read the last 4 KB of the console log of a job's latest build.", func(ctx context.Context, a jobArgs) (any, error) {
			b, ok, err := c.LastBuild(ctx, a.Job)
			if err != nil || !ok {
				return nil, tools.Errorf("no build to read: %v", err)
			}
			out, err := c.Console(ctx, a.Job, b.Number, 4096)
			if err != nil {
				return nil, tools.Errorf("console failed: %v", err)
			}
			return out, nil
		}),
	}
}
