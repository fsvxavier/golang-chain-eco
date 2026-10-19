// Command ciam indexes a project and serves its context to AI assistants:
// cheap hybrid search, ADR/PRD/plan documents and persistent memory, as a CLI
// and as an MCP server (VS Code, Cursor, Claude Code...).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/providers/ollama"
)

const usage = `ciam - context for AI assistants

  ciam index [path] [--type T] [--force] [--skip-docs]
  ciam search "query" [--type model,view] [--limit N] [--compress]
  ciam context "query"             search + compress (cheapest)
  ciam decision "query"            code + ADR + PRD + plans
  ciam remember "text" [--type decision]    |   ciam recall "query"
  ciam adr|prd|plan|research new "title" [--prd PRD-001]   |   ... list
  ciam adr supersede N "new title"
  ciam status
  ciam watch [path] [--interval 30s]
  ciam mcp                         MCP server on stdio
  ciam serve [--addr :8080]        MCP server over HTTP (POST /)
  ciam init [--editor vscode|cursor|claude] [--print]

Environment: CIAM_DB_PATH (default ~/.local/share/ciam/ciam.json), CIAM_PROJECT_PATH,
CIAM_OLLAMA_URL / OLLAMA_HOST, CIAM_OLLAMA_MODEL (nomic-embed-text), CIAM_CODE_MODEL
(enables ciam_draft), CIAM_EMBEDDER=fake (offline, keyword-grade embeddings).`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		fmt.Println(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "ciam:", err)
		os.Exit(1)
	}
}

// parse accepts flags before and after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func projectPath() string {
	if p := os.Getenv("CIAM_PROJECT_PATH"); p != "" {
		return p
	}
	return "."
}

func dbPath() string {
	if p := os.Getenv("CIAM_DB_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ciam", "ciam.json")
}

func newManager(ctx context.Context) (*ciam.Manager, error) {
	fs, err := ciam.OpenFileStore(dbPath())
	if err != nil {
		return nil, err
	}
	m := ciam.New(pickEmbedder(ctx), fs)
	if cm := os.Getenv("CIAM_CODE_MODEL"); cm != "" {
		m.Model = ollama.New(cm)
	}
	return m, nil
}

// pickEmbedder uses Ollama when it answers, otherwise a hashing embedder so
// the tool still works (ranking then leans on the BM25 half).
func pickEmbedder(ctx context.Context) embeddings.Embedder {
	if os.Getenv("CIAM_EMBEDDER") == "fake" {
		return embeddings.Fake{}
	}
	url := os.Getenv("CIAM_OLLAMA_URL")
	model := os.Getenv("CIAM_OLLAMA_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}
	e := &ollama.Embeddings{Model: model, BaseURL: url}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := e.EmbedQuery(pctx, "ping"); err != nil {
		fmt.Fprintf(os.Stderr, "ciam: Ollama embeddings unavailable (%v); using offline hashing embeddings\n", err)
		return embeddings.Fake{}
	}
	return e
}

func run(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case "adr", "prd", "plan", "research":
		return docsCmd(ciam.DocKind(cmd), args)
	case "init":
		return initCmd(args)
	}
	m, err := newManager(ctx)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	typ := fs.String("type", "", "project type (index) or chunk types (search/remember)")
	force := fs.Bool("force", false, "re-embed everything")
	skipDocs := fs.Bool("skip-docs", false, "do not index docs/")
	limit := fs.Int("limit", 5, "max results")
	compress := fs.Bool("compress", false, "compress results")
	interval := fs.Duration("interval", 30*time.Second, "watch interval")
	addr := fs.String("addr", ":8080", "listen address")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	root := func() string {
		if len(pos) > 0 {
			return pos[0]
		}
		return projectPath()
	}
	query := func() (string, error) {
		if len(pos) == 0 {
			return "", errors.New("a query is required")
		}
		return strings.Join(pos, " "), nil
	}
	project := ciam.ProjectID(projectPath())

	switch cmd {
	case "index":
		r, err := m.Index(ctx, root(), ciam.IndexOptions{Type: *typ, Force: *force, SkipDocs: *skipDocs})
		if err != nil {
			return err
		}
		fmt.Printf("indexed %s (%s): %d files (%d changed, %d unchanged, %d removed), %d chunks, ~%d tokens\n",
			r.Project, r.Type, r.Files, r.Changed, r.Unchanged, r.Removed, r.Chunks, r.Tokens)
	case "search", "context":
		q, err := query()
		if err != nil {
			return err
		}
		var types []string
		if *typ != "" {
			types = strings.Split(*typ, ",")
		}
		res, err := m.Search(ctx, ciam.SearchRequest{Project: project, Query: q, Types: types, Limit: *limit, Compress: *compress || cmd == "context"})
		if err != nil {
			return err
		}
		for _, r := range res {
			fmt.Printf("── %s [%s] chunk %d  score %.3f (vec %.2f, bm25 %.2f)\n%s\n\n", r.File, r.Type, r.Index, r.Score, r.Vector, r.BM25, r.Content)
		}
		if len(res) == 0 {
			fmt.Println("no results (is the project indexed? run: ciam index)")
		}
	case "decision":
		q, err := query()
		if err != nil {
			return err
		}
		out, err := m.DecisionContext(ctx, project, q, *limit)
		if err != nil {
			return err
		}
		fmt.Println(out)
	case "remember":
		q, err := query()
		if err != nil {
			return err
		}
		id, err := m.Remember(ctx, *typ, q)
		if err != nil {
			return err
		}
		fmt.Println("remembered", id)
	case "recall":
		q, err := query()
		if err != nil {
			return err
		}
		mem, err := m.Recall(ctx, q, *limit)
		if err != nil {
			return err
		}
		for _, x := range mem {
			fmt.Printf("[%s] %s  (%.2f)\n", x.Type, x.Content, x.Score)
		}
	case "status":
		st, err := m.Status(ctx)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
	case "watch":
		fmt.Fprintf(os.Stderr, "watching %s every %s (ctrl-c to stop)\n", root(), *interval)
		m.Watch(ctx, root(), ciam.IndexOptions{Type: *typ}, *interval, func(r ciam.IndexResult, err error) {
			if err != nil {
				fmt.Fprintln(os.Stderr, "ciam: watch:", err)
				return
			}
			fmt.Printf("%s: %d changed, %d removed\n", time.Now().Format("15:04:05"), r.Changed, r.Removed)
		})
	case "mcp":
		srv := ciam.Toolkit{M: m, Project: projectPath()}.MCPServer(nil)
		return srv.ServeStdio(ctx, os.Stdin, os.Stdout)
	case "serve":
		srv := ciam.Toolkit{M: m, Project: projectPath()}.MCPServer(nil)
		hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { <-ctx.Done(); _ = hs.Shutdown(context.Background()) }()
		fmt.Fprintln(os.Stderr, "MCP over HTTP on", *addr)
		if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q (try: ciam help)", cmd)
	}
	return nil
}

func docsCmd(kind ciam.DocKind, args []string) error {
	fs := flag.NewFlagSet(string(kind), flag.ContinueOnError)
	prd := fs.String("prd", "", "PRD id a plan implements, e.g. PRD-001")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: ciam %s new \"title\" | list%s", kind, map[bool]string{true: " | supersede N \"title\"", false: ""}[kind == ciam.ADR])
	}
	d := ciam.NewDocs(projectPath())
	switch pos[0] {
	case "new":
		if len(pos) < 2 {
			return errors.New("a title is required")
		}
		p, err := d.New(kind, strings.Join(pos[1:], " "), *prd)
		if err != nil {
			return err
		}
		fmt.Println("created", p)
		fmt.Println("fill it in, then run: ciam index")
	case "list":
		docs, err := d.List(kind)
		if err != nil {
			return err
		}
		for _, x := range docs {
			fmt.Printf("%s-%03d  %-11s %s\n", strings.ToUpper(string(kind)), x.Number, x.Status, x.Title)
		}
	case "supersede":
		if kind != ciam.ADR || len(pos) < 3 {
			return errors.New("usage: ciam adr supersede N \"new title\"")
		}
		var n int
		if _, err := fmt.Sscan(pos[1], &n); err != nil {
			return fmt.Errorf("N must be a number: %w", err)
		}
		p, err := d.SupersedeADR(n, strings.Join(pos[2:], " "))
		if err != nil {
			return err
		}
		fmt.Println("created", p)
	default:
		return fmt.Errorf("unknown subcommand %q", pos[0])
	}
	return nil
}

// initCmd writes the MCP configuration for an editor, merging with whatever
// servers are already configured.
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	editor := fs.String("editor", "", "vscode, cursor or claude (auto-detected when empty)")
	printOnly := fs.Bool("print", false, "print the configuration instead of writing it")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	root, _ := filepath.Abs(".")
	exe, err := os.Executable()
	if err != nil {
		exe = "ciam"
	}
	if *editor == "" {
		*editor = detectEditor(root)
	}
	type target struct{ file, key string }
	targets := map[string]target{
		"vscode": {".vscode/mcp.json", "servers"},
		"cursor": {".cursor/mcp.json", "mcpServers"},
		"claude": {".mcp.json", "mcpServers"},
	}
	t, ok := targets[*editor]
	if !ok {
		return fmt.Errorf("unsupported editor %q (vscode, cursor, claude); use --print and paste into your editor", *editor)
	}
	entry := map[string]any{"command": exe, "args": []string{"mcp"}, "env": map[string]string{"CIAM_PROJECT_PATH": root}}
	if *editor == "vscode" {
		entry["type"] = "stdio"
		entry["env"] = map[string]string{"CIAM_PROJECT_PATH": "${workspaceFolder}"}
	}
	path := filepath.Join(root, t.file)
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("%s exists but is not valid JSON: %w", t.file, err)
		}
	}
	servers, _ := cfg[t.key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["ciam"] = entry
	cfg[t.key] = servers
	out, _ := json.MarshalIndent(cfg, "", "  ")
	if *printOnly {
		fmt.Println(string(out))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (editor: %s). Next: ciam index\n", t.file, *editor)
	return nil
}

func detectEditor(root string) string {
	for _, c := range []struct{ dir, name string }{{".cursor", "cursor"}, {".vscode", "vscode"}} {
		if st, err := os.Stat(filepath.Join(root, c.dir)); err == nil && st.IsDir() {
			return c.name
		}
	}
	return "vscode"
}
