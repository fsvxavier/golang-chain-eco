// Command knowledge indexes a folder of documents and answers questions over
// it with RAG (vector search), CAG (the whole corpus in context) or KAG (a
// knowledge graph), as a CLI and as an MCP server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/cag"
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/kag"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

const usage = `knowledge - answers from your documents with RAG, CAG or KAG

  knowledge index <path> [--glob "*.md"] [--kind rag|kag|both] [--out .knowledge]
  knowledge ask "question" [--mode auto|rag|cag|kag] [--dir .knowledge] [--src path] [--glob "*.md"]
  knowledge mcp [--dir .knowledge]                    MCP server on stdio
  knowledge serve [--addr :8080] [--dir .knowledge]   MCP server over HTTP (POST /)

Environment: KNOWLEDGE_MODEL (provider:model; default anthropic:claude-sonnet-5-5 with
ANTHROPIC_API_KEY, openai:gpt-4o-mini with OPENAI_API_KEY, otherwise an offline fake that
echoes the top source), KNOWLEDGE_EMBEDDINGS (provider:model, e.g. ollama:nomic-embed-text;
default offline hashing embeddings).`

const (
	defaultDir    = ".knowledge"
	vectorsFile   = "vectors.json"
	graphFile     = "graph.json"
	contextWindow = 200000
	ragK          = 4
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "knowledge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	return runWith(args, stdout, os.Stderr, deps{})
}

// deps lets tests inject the chat model and the embedder. Zero values are
// resolved from the environment.
type deps struct {
	chat models.ChatModel
	emb  embeddings.Embedder
}

func runWith(args []string, stdout, stderr io.Writer, d deps) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "index":
		return cmdIndex(rest, stdout, stderr, d)
	case "ask":
		return cmdAsk(rest, stdout, stderr, d)
	case "mcp", "serve":
		return cmdServe(cmd, rest, stderr, d)
	}
	return fmt.Errorf("unknown command %q (try: knowledge help)", cmd)
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

func cmdIndex(args []string, stdout, stderr io.Writer, d deps) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	glob := fs.String("glob", "*.md", "file pattern inside the folder")
	kind := fs.String("kind", "rag", "what to build: rag, kag or both")
	out := fs.String("out", defaultDir, "directory for vectors.json and graph.json")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: knowledge index <path> [--glob \"*.md\"] [--kind rag|kag|both] [--out dir]")
	}
	if *kind != "rag" && *kind != "kag" && *kind != "both" {
		return fmt.Errorf("unknown --kind %q (want rag, kag or both)", *kind)
	}
	emb, err := d.embedder()
	if err != nil {
		return err
	}
	ctx := context.Background()

	vs := vectorstores.NewMemory(emb)
	ix := rag.NewIndexer(vs, nil)
	if _, err := ix.Index(ctx, documents.Directory(pos[0], *glob, nil)); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := vs.Save(filepath.Join(*out, vectorsFile)); err != nil {
		return err
	}
	chunks := ix.Documents()
	files := map[string]bool{}
	for _, c := range chunks {
		files[fmt.Sprint(c.Metadata["source"])] = true
	}
	fmt.Fprintf(stdout, "rag: %d files, %d chunks -> %s\n", len(files), len(chunks), filepath.Join(*out, vectorsFile))
	if *kind == "rag" {
		return nil
	}

	m, err := d.model(stderr)
	if err != nil {
		return err
	}
	// Chunk IDs are vector-store IDs; clear them so provenance comes from metadata["source"].
	docs := make([]documents.Document, len(chunks))
	for i, c := range chunks {
		c.ID = ""
		docs[i] = c
	}
	g := kag.NewMemory()
	stats, err := (&kag.Extractor{Model: m, Embedder: emb, Graph: g}).Extract(ctx, docs)
	if err != nil {
		fmt.Fprintf(stdout, "kag: warning: %v\n", err)
	}
	if err := g.Save(filepath.Join(*out, graphFile)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "kag: %d entities, %d relations (%d dropped) -> %s\n",
		stats.Entities, stats.Relations, stats.Dropped, filepath.Join(*out, graphFile))
	return nil
}

func cmdAsk(args []string, stdout, stderr io.Writer, d deps) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	mode := fs.String("mode", "auto", "auto, rag, cag or kag")
	dir := fs.String("dir", defaultDir, "directory with vectors.json and graph.json")
	src := fs.String("src", "", "corpus for CAG: a folder (or file) put whole in context")
	glob := fs.String("glob", "*.md", "file pattern for --src")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("usage: knowledge ask \"question\" [--mode auto|rag|cag|kag] [--dir dir] [--src path]")
	}
	question := strings.Join(pos, " ")
	force := ""
	switch *mode {
	case "auto":
	case "rag", "cag", "kag":
		force = *mode
	default:
		return fmt.Errorf("unknown --mode %q (want auto, rag, cag or kag)", *mode)
	}
	if *mode == "kag" {
		if _, err := os.Stat(filepath.Join(*dir, graphFile)); err != nil {
			return fmt.Errorf("kag needs %s in %s: run knowledge index --kind kag first", graphFile, *dir)
		}
	}
	if *mode == "cag" && *src == "" {
		return errors.New("cag needs --src: the folder whose documents go in the context")
	}

	ctx := context.Background()
	k, err := loadKB(ctx, *dir, *src, *glob, d, stderr)
	if err != nil {
		return err
	}
	ans, err := k.router(*mode == "auto").Answer(ctx, knowledge.Question{Text: question, Force: force})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, ans.Text)
	if len(ans.Sources) > 0 {
		fmt.Fprintln(stdout, "\nsources:")
		for i, s := range ans.Sources {
			fmt.Fprintf(stdout, "  [%d] %s  %v\n", i+1, s.ID, s.Metadata["source"])
		}
	}
	fmt.Fprintln(stdout, "\ntrace:")
	for _, st := range ans.Trace {
		fmt.Fprintf(stdout, "  %s: %s\n", st.Name, st.Detail)
	}
	return nil
}

func cmdServe(cmd string, args []string, stderr io.Writer, d deps) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dir := fs.String("dir", defaultDir, "directory with vectors.json and graph.json")
	addr := fs.String("addr", ":8080", "listen address (serve)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	ctx := context.Background()
	k, err := loadKB(ctx, *dir, "", "*.md", d, stderr)
	if err != nil {
		return err
	}
	srv := k.toolkit().MCPServer(nil)
	if cmd == "mcp" {
		return srv.ServeStdio(ctx, os.Stdin, os.Stdout)
	}
	sctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-sctx.Done(); _ = hs.Shutdown(context.Background()) }()
	fmt.Fprintln(stderr, "knowledge serve listening on", *addr)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// kb holds the answerers built from one index directory (and optional corpus).
// Unset strategies are nil interfaces, so knowledge.Router and Toolkit skip them.
type kb struct {
	rag, cag, kag knowledge.Answerer
	profile       knowledge.Profile
}

func loadKB(ctx context.Context, dir, src, glob string, d deps, stderr io.Writer) (*kb, error) {
	emb, err := d.embedder()
	if err != nil {
		return nil, err
	}
	vs, err := vectorstores.Load(filepath.Join(dir, vectorsFile), emb)
	if err != nil {
		return nil, fmt.Errorf("no index in %s (run: knowledge index <path> --out %s): %w", dir, dir, err)
	}
	m, err := d.model(stderr)
	if err != nil {
		return nil, err
	}
	pipe := &rag.Pipeline{Store: vs, Model: m, K: ragK}
	k := &kb{rag: pipe, profile: knowledge.Profile{ContextWindow: contextWindow}}

	if _, err := os.Stat(filepath.Join(dir, graphFile)); err == nil {
		g, err := kag.LoadMemory(filepath.Join(dir, graphFile))
		if err != nil {
			return nil, err
		}
		k.kag = &kag.Engine{Model: m, Graph: g, Embedder: emb, Chunks: pipe}
		k.profile.HasGraph = true
	}
	if src != "" {
		docs, err := documents.Directory(src, glob, nil).Load(ctx)
		if err != nil {
			return nil, err
		}
		c := cag.NewCorpus(docs)
		k.cag = &cag.Engine{Model: m, Corpus: c, Window: contextWindow}
		k.profile.CorpusTokens = c.Tokens(knowledge.Heuristic{})
	}
	return k, nil
}

// router picks the strategy per question; fallback retries with RAG on error.
func (k *kb) router(fallback bool) *knowledge.Router {
	return &knowledge.Router{RAG: k.rag, CAG: k.cag, KAG: k.kag, Profile: k.profile, Fallback: fallback}
}

func (k *kb) toolkit() knowledge.Toolkit {
	return knowledge.Toolkit{RAG: k.rag, CAG: k.cag, KAG: k.kag, Router: k.router(true)}
}

// embedder returns the injected embedder, or KNOWLEDGE_EMBEDDINGS, or the offline hashing embedder.
func (d deps) embedder() (embeddings.Embedder, error) {
	if d.emb != nil {
		return d.emb, nil
	}
	spec := os.Getenv("KNOWLEDGE_EMBEDDINGS")
	if spec == "" {
		return embeddings.Fake{Dim: 256}, nil
	}
	return chatmodels.InitEmbeddings(spec)
}

// model returns the injected model, or KNOWLEDGE_MODEL, or a provider picked
// from the API keys, or the offline fake (with a warning on stderr).
func (d deps) model(stderr io.Writer) (models.ChatModel, error) {
	if d.chat != nil {
		return d.chat, nil
	}
	spec := os.Getenv("KNOWLEDGE_MODEL")
	if spec == "" {
		switch {
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			spec = "anthropic:claude-sonnet-5-5"
		case os.Getenv("OPENAI_API_KEY") != "":
			spec = "openai:gpt-4o-mini"
		}
	}
	if spec == "" {
		fmt.Fprintln(stderr, "knowledge: warning: no API key or KNOWLEDGE_MODEL; using the offline fake model (answers echo the top source)")
		return offlineModel(), nil
	}
	return chatmodels.Init(spec)
}

// offlineModel stands in for a chat model without a key. It answers with the
// top numbered source, and gives extraction an empty payload so that
// index --kind kag still runs; planning fails with a clear message.
func offlineModel() models.ChatModel {
	return &models.Fake{Name: "offline:echo", Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		if len(o.Tools) > 0 {
			if o.Tools[0].Name == "extraction" {
				return messages.AI("", messages.ToolCall{ID: "offline", Name: "extraction",
					Args: map[string]any{"entities": []any{}, "relations": []any{}}}), nil
			}
			return messages.Message{}, errors.New("offline model cannot plan; set KNOWLEDGE_MODEL")
		}
		for _, line := range strings.Split(msgs[0].Text(), "\n") {
			if rest, ok := strings.CutPrefix(line, "[1] "); ok {
				return messages.AI(rest + " [1]"), nil
			}
		}
		return messages.AI("No matching context was found."), nil
	}}
}
