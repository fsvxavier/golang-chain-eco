// Package backend gives the PO read-only access to the backend a demand names:
// backend_read and backend_grep over its source roots, backend_get over its
// URLs. Every call is checked against the request's scope and fails closed:
// no scope, or anything outside it, is a tool error.
package backend

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

// Config bounds the tools. Zero values take the defaults.
type Config struct {
	WorkspaceRoot string        // every root must lie under it (default WORKSPACE_ROOT, else the cwd)
	MaxBytes      int           // cap on any output (default 1 MB)
	Timeout       time.Duration // per HTTP call (default 10 s)
	// Client is cloned; its redirect policy and dialing are always replaced by
	// the guarded ones, so it cannot widen what backend_get reaches.
	Client *http.Client
	// Report is told about every call that passed the guards and got an answer,
	// with the request id the tools resolved. RoleTools defaults it to the hub's
	// AddCall; Tools has no request id and defaults to no report. A failure is
	// logged and never fails the tool call: the hub's count is then short, which
	// the manager treats as unverified.
	Report func(ctx context.Context, requestID string) error
	// Run executes a code graph engine (codegraph, graphify) for backend_graph.
	// Nil runs the real binary without a shell, with a timeout and a size cap.
	Run func(ctx context.Context, name string, args []string) (string, error)
}

func (c Config) withDefaults() Config {
	if c.WorkspaceRoot == "" {
		c.WorkspaceRoot = os.Getenv("WORKSPACE_ROOT")
	}
	if c.WorkspaceRoot == "" {
		c.WorkspaceRoot, _ = os.Getwd()
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = 1 << 20
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	return c
}

// Tools returns backend_read, backend_grep and backend_get, guarded by the
// scope scope returns for the call's context. They have no request id, so
// Report (if set) gets "" as the id; RoleTools reports per request.
func Tools(scope func(ctx context.Context) (team.Scope, error), cfg Config) []tools.Tool {
	return newTools(func(ctx context.Context) (string, team.Scope, error) {
		s, err := scope(ctx)
		return "", s, err
	}, cfg)
}

// RoleTools is a team.LocalOptions.RoleTools that gives the po role the
// backend tools and every other role backend_graph. The scope and request id come from the hub by the job's exact
// thread; hub may return nil until the team is up, which means no access.
func RoleTools(hub func() *team.HubClient, cfg Config) func(role string) []tools.Tool {
	if cfg.Report == nil {
		cfg.Report = func(ctx context.Context, id string) error {
			if h := hub(); h != nil {
				return h.AddCall(ctx, id)
			}
			return errors.New("no hub client")
		}
	}
	ts := newTools(func(ctx context.Context) (string, team.Scope, error) {
		return team.RequestScopeFromContext(ctx, hub())
	}, cfg)
	// Every role may ask a root's code graph (it only reads, within the demand's
	// roots, and saves tokens in any role's searches); the other tools are the PO's.
	var graph []tools.Tool
	for _, t := range ts {
		if t.Name() == "backend_graph" {
			graph = append(graph, t)
		}
	}
	return func(role string) []tools.Tool {
		if role == "po" {
			return ts
		}
		return graph
	}
}

type readArgs struct {
	Path   string `json:"path" jsonschema:"file or directory, relative to the first root or absolute inside a root"`
	Offset int    `json:"offset,omitempty" jsonschema:"byte offset to continue a file that ended with [truncated]"`
}

// maxRead caps one backend_read: every byte stays in the history and is billed
// again on each later step, so a big file is read in pages, or searched with
// backend_grep, instead of dumped whole.
const maxRead = 32 << 10

type grepArgs struct {
	Pattern string `json:"pattern" jsonschema:"Go regular expression"`
	Path    string `json:"path" jsonschema:"file or directory to search, relative to the first root or absolute inside a root"`
}

// getArgs has no method on purpose: backend_get can only GET.
type getArgs struct {
	URL string `json:"url" jsonschema:"http(s) URL on a host the demand named"`
}

func newTools(resolve func(context.Context) (string, team.Scope, error), cfg Config) []tools.Tool {
	cfg = cfg.withDefaults()
	count := func(ctx context.Context, id string) {
		if cfg.Report == nil {
			return
		}
		if err := cfg.Report(ctx, id); err != nil {
			log.Printf("backend: counting a call for %q: %v (the manager will see too few calls)", id, err)
		}
	}
	// scope returns the request id and scope, refusing when the demand named no backend.
	scope := func(ctx context.Context, tool string) (string, team.Scope, error) {
		id, s, err := resolve(ctx)
		if err != nil {
			return "", team.Scope{}, tools.Errorf("%s: no backend scope for this job (%v): access refused", tool, err)
		}
		if len(s.Roots)+len(s.URLs) == 0 {
			return "", team.Scope{}, tools.Errorf("%s: the demand named no backend: access refused", tool)
		}
		return id, s, nil
	}
	return []tools.Tool{
		tools.New("backend_read", "Read one file (or list one directory) of the backend source the demand named, at most 32 KB per call (use offset to page, backend_grep to search big files). Read-only; use it to see endpoints, payloads and errors as written.",
			func(ctx context.Context, a readArgs) (any, error) {
				id, s, err := scope(ctx, "backend_read")
				if err != nil {
					return nil, err
				}
				p, err := resolvePath(cfg.WorkspaceRoot, s.Roots, a.Path)
				if err != nil {
					return nil, tools.Errorf("backend_read: %v", err)
				}
				out, err := read(p, min(cfg.MaxBytes, maxRead), a.Offset)
				if err != nil {
					return nil, tools.Errorf("backend_read: %v", err)
				}
				count(ctx, id)
				return out, nil
			}),
		tools.New("backend_grep", "Search the backend source the demand named for a regular expression; returns path:line: text. Read-only; never follows symlinks.",
			func(ctx context.Context, a grepArgs) (any, error) {
				id, s, err := scope(ctx, "backend_grep")
				if err != nil {
					return nil, err
				}
				re, err := regexp.Compile(a.Pattern)
				if err != nil {
					return nil, tools.Errorf("backend_grep: pattern: %v", err)
				}
				p, err := resolvePath(cfg.WorkspaceRoot, s.Roots, a.Path)
				if err != nil {
					return nil, tools.Errorf("backend_grep: %v", err)
				}
				out, err := grep(ctx, p, re, cfg.MaxBytes)
				if err != nil {
					return nil, tools.Errorf("backend_grep: %v", err)
				}
				count(ctx, id)
				return out, nil
			}),
		graphTool(cfg, resolve, count),
		tools.New("backend_get", "HTTP GET one URL of the backend the demand named (GET only); returns status, content type and body. Use it to see real responses and the API description.",
			func(ctx context.Context, a getArgs) (any, error) {
				id, s, err := scope(ctx, "backend_get")
				if err != nil {
					return nil, err
				}
				out, err := get(ctx, cfg, s.URLs, a.URL)
				if err != nil {
					return nil, tools.Errorf("backend_get: %v", err)
				}
				count(ctx, id)
				return out, nil
			}),
	}
}

// resolved returns the absolute path with every symlink resolved.
func resolved(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// within reports whether p is base or below it (both already resolved).
func within(p, base string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvePath resolves p (relative to the first root, or absolute) and requires
// the real path under a root that itself lies under the workspace.
func resolvePath(workspace string, roots []string, p string) (string, error) {
	ws, err := resolved(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace root: %v", err)
	}
	var allowed []string
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue // an empty root grants nothing
		}
		if !filepath.IsAbs(r) {
			r = filepath.Join(ws, r) // relative roots hang off the workspace, not the cwd
		}
		if rr, err := resolved(r); err == nil && within(rr, ws) {
			allowed = append(allowed, rr)
		}
	}
	if len(allowed) == 0 {
		return "", errors.New("no named root lies inside the workspace: access refused")
	}
	if p == "" {
		p = "."
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return "", fmt.Errorf("%q: '..' is not allowed: access refused", p)
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(allowed[0], p)
	}
	rp, err := resolved(p)
	if err != nil {
		return "", fmt.Errorf("%q: not found inside the named roots", p)
	}
	for _, r := range allowed {
		if within(rp, r) {
			return rp, nil
		}
	}
	return "", fmt.Errorf("%q is outside the named roots: access refused", p)
}

const truncated = "\n[truncated]"

// capped reads at most max bytes of r, marking a cut.
func capped(r io.Reader, max int) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return "", err
	}
	if len(b) > max {
		return string(b[:max]) + truncated, nil
	}
	return string(b), nil
}

func read(p string, max, offset int) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		es, err := os.ReadDir(p)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, e := range es {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			b.WriteString(name + "\n")
		}
		return capped(strings.NewReader(b.String()), max)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
			return "", err
		}
	}
	out, err := capped(f, max)
	if strings.HasSuffix(out, truncated) {
		out += fmt.Sprintf(" at byte %d of %d; call again with offset=%d, or use backend_grep", offset+max, info.Size(), offset+max)
	}
	return out, err
}

// maxGrepFiles caps the files one grep opens, so a node_modules tree cannot
// keep it busy past its usefulness.
var maxGrepFiles = 5000

// grep walks p without following symlinks (WalkDir never descends into them,
// and symlinked files are skipped). It stops when ctx ends, once the output
// reaches max, or after maxGrepFiles files.
func grep(ctx context.Context, p string, re *regexp.Regexp, max int) (string, error) {
	var b strings.Builder
	full, files := false, 0
	base := filepath.Dir(p)
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		base = p
	}
	err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || full {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		if files >= maxGrepFiles {
			full = true
			fmt.Fprintf(&b, "[truncated: stopped after %d files]\n", maxGrepFiles)
			return filepath.SkipAll
		}
		files++
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		rel, _ := filepath.Rel(base, path)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if re.Match(sc.Bytes()) {
				fmt.Fprintf(&b, "%s:%d: %s\n", rel, n, sc.Text())
				if b.Len() > max {
					full = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if b.Len() == 0 {
		return "no matches", nil
	}
	return capped(strings.NewReader(b.String()), max)
}

// hostport is the lower-case host:port of u, with the scheme's default port.
func hostport(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// checkURL allows http(s) URLs whose host:port equals one the demand named.
func checkURL(u *url.URL, allowed map[string]bool) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q: only http and https: access refused", u.Scheme)
	}
	if u.User != nil {
		return errors.New("credentials in the URL are not allowed: access refused")
	}
	if !allowed[hostport(u)] {
		return fmt.Errorf("host %s was not named by the demand: access refused", u.Host)
	}
	return nil
}

// specialIP reports addresses a backend tool must never reach by accident:
// loopback, link-local (which holds 169.254.169.254), unspecified, and the
// cloud metadata addresses outside link-local. RFC 1918 ranges are allowed:
// compose service names resolve there.
func specialIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return true
	}
	switch s {
	case "100.100.100.200", "fd00:ec2::254": // Alibaba and AWS IPv6 metadata
		return true
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// checkDial vets the resolved address a dial for name actually connects to,
// so a name that resolves (or rebinds) to a special IP is caught. The name
// localhost (already checked as named) may reach loopback IPs and nothing
// else; any other special IP needs its ip:port named as a literal.
func checkDial(name, address string, literal map[string]bool) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if strings.EqualFold(name, "localhost") {
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("localhost resolved to %s, which is not loopback: access refused", address)
		}
		return nil
	}
	if specialIP(host) && !literal[net.JoinHostPort(ip.String(), port)] {
		return fmt.Errorf("address %s is loopback, link-local or metadata and was not named: access refused", address)
	}
	return nil
}

func get(ctx context.Context, cfg Config, named []string, raw string) (string, error) {
	allowed := map[string]bool{}
	literal := map[string]bool{} // ip:port named as an IP literal: the only way to reach a special IP
	for _, n := range named {
		u, err := url.Parse(n)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			continue
		}
		hp := hostport(u)
		allowed[hp] = true
		if net.ParseIP(u.Hostname()) != nil {
			literal[hp] = true
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("url: %v", err)
	}
	if err := checkURL(u, allowed); err != nil {
		return "", err
	}
	tr := &http.Transport{
		Proxy: nil, // a proxy would make the dial check see the proxy, not the target
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			h, p, err := net.SplitHostPort(addr)
			if err != nil || !allowed[net.JoinHostPort(strings.ToLower(h), p)] {
				return nil, fmt.Errorf("dial %s: not named by the demand: access refused", addr)
			}
			d := &net.Dialer{Timeout: cfg.Timeout, Control: func(_, address string, _ syscall.RawConn) error {
				return checkDial(h, address, literal)
			}}
			return d.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{}
	if cfg.Client != nil {
		*client = *cfg.Client
	}
	client.Transport, client.Timeout = tr, cfg.Timeout
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return checkURL(r.URL, allowed)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := capped(resp.Body, cfg.MaxBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\nContent-Type: %s\n\n%s", resp.StatusCode, resp.Header.Get("Content-Type"), body), nil
}
