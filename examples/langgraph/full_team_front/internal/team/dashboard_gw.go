package team

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/tasktrack"
)

const regTTL = 30 * time.Second // a project that stops re-registering drops off the list

// gateway is the dashboard that owns DASHBOARD_ADDR. It serves its own project
// and proxies the others, which register from their own loopback listeners, so
// one URL covers every project: /p/<project>/.
type gateway struct {
	own  project
	name string

	mu     sync.Mutex
	others map[string]*remoteProject
}

type remoteProject struct {
	Name, Root string
	URL        *url.URL
	seen       time.Time
}

func newGateway(own project) *gateway {
	return &gateway{own: own, name: filepath.Base(own.root), others: map[string]*remoteProject{}}
}

// register adds or refreshes a project and returns the name it got. The same
// root keeps its name; a different root with a taken name gets a short hash.
func (g *gateway) register(name, root string, u *url.URL) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if name == g.name && root != g.own.root || name != g.name && g.others[name] != nil && g.others[name].Root != root {
		sum := sha1.Sum([]byte(root))
		name += "-" + hex.EncodeToString(sum[:2])
	}
	g.others[name] = &remoteProject{Name: name, Root: root, URL: u, seen: time.Now()}
	return name
}

func (g *gateway) live() []*remoteProject {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*remoteProject
	for n, r := range g.others {
		if time.Since(r.seen) > regTTL {
			delete(g.others, n)
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *remoteProject) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// ProjectInfo is one entry of the project selector.
type ProjectInfo struct {
	Name     string `json:"name"`
	Root     string `json:"root"`
	Alive    bool   `json:"alive"`
	Requests int    `json:"requests"`
	Tokens   int    `json:"tokens"`
}

func summary(name, root string, d *DashState) ProjectInfo {
	p := ProjectInfo{Name: name, Root: root}
	if d != nil {
		p.Alive, p.Requests, p.Tokens = true, len(d.Requests), d.Tokens.Total
	}
	return p
}

func (g *gateway) projects(ctx context.Context) []ProjectInfo {
	own := g.own.state(ctx)
	out := []ProjectInfo{summary(g.name, g.own.root, &own)}
	rs := g.live()
	infos := make([]ProjectInfo, len(rs))
	var wg sync.WaitGroup
	for i, r := range rs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			var d *DashState
			if req, err := http.NewRequestWithContext(cctx, "GET", r.URL.String()+"/api/state", nil); err == nil {
				if resp, err := http.DefaultClient.Do(req); err == nil {
					var s DashState
					if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&s) == nil {
						d = &s
					}
					resp.Body.Close()
				}
			}
			infos[i] = summary(r.Name, r.Root, d)
		}()
	}
	wg.Wait()
	return append(out, infos...)
}

func loopbackURL(s string) (*url.URL, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "http" || u.Path != "" {
		return nil, false
	}
	ip := net.ParseIP(u.Hostname())
	return u, ip != nil && ip.IsLoopback()
}

func (g *gateway) handler() http.Handler {
	own := g.own.handler()
	mux := http.NewServeMux()
	jsonOut := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("project") // /?project=<name> is the same as /p/<name>/
		if name == "" {
			name = g.name
		}
		http.Redirect(w, r, "/p/"+url.PathEscape(name)+"/", http.StatusFound)
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, g.projects(r.Context())) })
	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		// Only loopback peers, and only loopback URLs: the gateway proxies to what
		// is registered, so anything else would make it a relay to other hosts.
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		var b struct{ Name, Root, URL string }
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b) != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		u, ok := loopbackURL(b.URL)
		if !ok || b.Name == "" || strings.ContainsAny(b.Name, "/\\") {
			http.Error(w, "bad project", http.StatusBadRequest)
			return
		}
		jsonOut(w, map[string]string{"name": g.register(b.Name, b.Root, u)})
	})
	mux.HandleFunc("/p/{name}/", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		r2 := r.Clone(r.Context())
		r2.URL.Path, r2.URL.RawPath = "/"+strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/p/"+name), "/"), ""
		if name == g.name {
			own.ServeHTTP(w, r2)
			return
		}
		for _, rp := range g.live() {
			if rp.Name == name {
				httputil.NewSingleHostReverseProxy(rp.URL).ServeHTTP(w, r2)
				return
			}
		}
		http.Error(w, "unknown project "+name, http.StatusNotFound)
	})
	mux.HandleFunc("GET /p/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
	})
	return mux
}

// startDashboard serves the progress page on DASHBOARD_ADDR (host:port), if
// set. The first team to bind it owns it and lists every project; a team that
// finds it taken serves on a free loopback port and registers there, so the
// owner's /p/<project>/ shows it too.
// shortcut: if the owner exits, the others re-register to whoever binds the
// address next, but until then their dashboards are reachable only by their own log URL.
func startDashboard(ctx context.Context, track *tasktrack.Tracker, hub *HubClient) {
	addr := os.Getenv("DASHBOARD_ADDR")
	if addr == "" {
		return
	}
	// The team reads backends under WORKSPACE_ROOT, else the cwd: that is its project.
	root := os.Getenv("WORKSPACE_ROOT")
	if root == "" {
		root, _ = os.Getwd()
	}
	p := project{track, hub, root}
	if ln, err := net.Listen("tcp", addr); err == nil {
		log.Printf("dashboard: http://%s/p/%s/ (project %s)", ln.Addr(), url.PathEscape(filepath.Base(root)), root)
		go func() { _ = serve(ctx, ln, newGateway(p).handler()) }()
		return
	}
	ln, own, err := listen()
	if err != nil {
		log.Printf("dashboard: %v (continuing without it)", err)
		return
	}
	log.Printf("dashboard: %s is taken; registering %s with its owner (own page %s)", addr, root, own)
	go func() { _ = serve(ctx, ln, p.handler()) }()
	go func() {
		first := true
		for {
			name, err := registerWith(ctx, addr, filepath.Base(root), root, own)
			if first && err == nil {
				log.Printf("dashboard: http://%s/p/%s/", addr, url.PathEscape(name))
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(regTTL / 3):
			}
		}
	}()
}

func registerWith(ctx context.Context, owner, name, root, own string) (string, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "root": root, "url": own})
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+owner+"/api/register", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct{ Name string }
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil {
		return "", fmt.Errorf("register: http %d", resp.StatusCode)
	}
	return out.Name, nil
}
