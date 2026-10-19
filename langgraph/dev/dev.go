// Package dev serves compiled graphs over the Agent Protocol for local
// development: request logging, a printed URL, and a warning for graphs
// without a checkpointer. The user writes the main package:
//
//	func main() {
//		g, _ := agents.Create(agents.Options{Model: m, Tools: ts,
//			Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
//		log.Fatal(dev.Serve("127.0.0.1:2024", map[string]*graph.Compiled{"my_agent": g}))
//	}
//
// Ctrl-C stops the process; there is no graceful drain.
package dev

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Option configures Serve.
type Option func(*config)

type config struct {
	logger *log.Logger
	out    io.Writer
}

// WithLogger sets where request logs go (default: stderr).
func WithLogger(l *log.Logger) Option { return func(c *config) { c.logger = l } }

// WithOutput sets where the startup banner goes (default: stdout).
func WithOutput(w io.Writer) Option { return func(c *config) { c.out = w } }

// Serve listens on addr (":0" picks a free port), prints the base URL and
// the agent IDs, and serves until the listener fails. Each graph becomes an
// agent whose ID is its map key.
func Serve(addr string, graphs map[string]*graph.Compiled, opts ...Option) error {
	cfg := config{logger: log.New(os.Stderr, "", log.LstdFlags), out: os.Stdout}
	for _, o := range opts {
		o(&cfg)
	}
	ids := make([]string, 0, len(graphs))
	for id := range graphs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	specs := make([]agentprotocol.AgentSpec, 0, len(ids))
	for _, id := range ids {
		if graphs[id] == nil {
			return fmt.Errorf("dev: agent %q has a nil graph", id)
		}
		if graphs[id].Saver() == nil {
			cfg.logger.Printf("warning: agent %q has no checkpointer; threads will not keep state (compile with graph.WithCheckpointer)", id)
		}
		specs = append(specs, agentprotocol.AgentSpec{ID: id, Graph: graphs[id]})
	}
	srv, err := agentprotocol.New(agentprotocol.Options{Agents: specs})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.out, "langgraph dev: http://%s\n", ln.Addr())
	fmt.Fprintf(cfg.out, "agents: %v\n", ids)
	return http.Serve(ln, logRequests(cfg.logger, srv))
}

func logRequests(l *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rw, r)
		l.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.code, time.Since(start).Round(time.Microsecond))
	})
}

// statusWriter records the status code. Flush is forwarded so SSE streams work.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) {
	s.code = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
