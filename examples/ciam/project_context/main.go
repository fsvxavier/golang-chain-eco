// project_context: give an AI assistant cheap, precise context about a
// codebase. Index a small Django-style project (plus its ADR/PRD documents),
// then search it with hybrid ranking, compress results, ask for the full
// decision context, remember decisions, and serve all of it over MCP.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/mcp"
)

var files = map[string]string{
	"manage.py":         "import os\n",
	"billing/apps.py":   "class BillingConfig: pass\n",
	"billing/models.py": "from django.db import models\n\nclass Invoice(models.Model):\n    \"\"\"An invoice issued to a customer.\"\"\"\n    # amount in cents, never floats\n    total_cents = models.IntegerField()\n    stripe_customer_id = models.CharField(max_length=64)\n",
	"billing/views.py":  "class InvoiceViewSet(ViewSet):\n    def list(self, request):\n        return Response(Invoice.objects.all())\n",
	"billing/tasks.py":  "@shared_task\ndef send_receipt_email(invoice_id):\n    \"\"\"Email the PDF receipt.\"\"\"\n    pass\n",
	"billing/tests.py":  "def test_total_is_integer():\n    assert True\n",
	"users/apps.py":     "class UsersConfig: pass\n",
	"users/models.py":   "from django.db import models\n\nclass Profile(models.Model):\n    email = models.EmailField(unique=True)\n",
}

func main() {
	ctx := context.Background()
	root := filepath.Join(must(os.MkdirTemp("", "ciam-demo-")), "shop")
	defer os.RemoveAll(filepath.Dir(root))
	for p, c := range files {
		full := filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		must0(os.WriteFile(full, []byte(c), 0o644))
	}

	// knowledge first: write down the WHY, then the code can be searched with it
	docs := ciam.NewDocs(root)
	adr := must(docs.New(ciam.ADR, "Store money as integer cents", ""))
	must0(os.WriteFile(adr, []byte("---\nstatus: accepted\ndate: 2026-03-01\ntitle: \"Store money as integer cents\"\n---\n\n# ADR-001\n\n## Decision\n\nAll amounts are integers in cents. Floats caused rounding errors in invoices (see incident 118).\n"), 0o644))
	must(docs.New(ciam.PRD, "Billing via Stripe", ""))
	must(docs.New(ciam.Plan, "Stripe integration", "PRD-001"))

	// an embedder: Ollama in real life (providers/ollama.Embeddings); a hashing one keeps this offline
	m := ciam.New(embeddings.Fake{}, nil)

	demo.Title("index (incremental)")
	r := must(m.Index(ctx, root, ciam.IndexOptions{}))
	fmt.Printf("%s (%s): %d files, %d chunks, ~%d tokens\n", r.Project, r.Type, r.Files, r.Chunks, r.Tokens)
	r = must(m.Index(ctx, root, ciam.IndexOptions{}))
	fmt.Printf("again: %d changed, %d unchanged (nothing re-embedded)\n", r.Changed, r.Unchanged)
	must0(os.WriteFile(filepath.Join(root, "billing/tasks.py"), []byte(files["billing/tasks.py"]+"\n@shared_task\ndef send_invoice_pdf(invoice_id):\n    pass\n"), 0o644))
	r = must(m.Index(ctx, root, ciam.IndexOptions{}))
	fmt.Printf("after editing one file: %d changed, %d unchanged\n", r.Changed, r.Unchanged)

	demo.Title("hybrid search: keywords + meaning, filtered by chunk type")
	for _, q := range []struct {
		query string
		types []string
	}{{"stripe customer id", nil}, {"send receipt", []string{"task"}}, {"why integers for money", []string{"adr"}}} {
		res := must(m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: q.query, Types: q.types, Limit: 1}))
		fmt.Printf("%-28s types=%-8v -> %s [%s] vec=%.2f bm25=%.2f\n", q.query, q.types, res[0].File, res[0].Type, res[0].Vector, res[0].BM25)
	}

	demo.Title("compression keeps signatures, drops docs and comments")
	raw := must(m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "Invoice total_cents", Types: []string{"model"}, Limit: 1}))
	cmp := must(m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "Invoice total_cents", Types: []string{"model"}, Limit: 1, Compress: true}))
	fmt.Printf("raw %d bytes -> compressed %d bytes\n%s\n", len(raw[0].Content), len(cmp[0].Content), cmp[0].Content)

	demo.Title("decision context: code + the reasons behind it, in one call")
	dc := must(m.DecisionContext(ctx, "shop", "invoice total cents stored", 1))
	for _, l := range strings.Split(dc, "\n") {
		if strings.HasPrefix(l, "**") || strings.HasPrefix(l, "# Decision") {
			fmt.Println(" ", l)
		}
		for _, sec := range []string{"## Code", "## Architecture decisions", "## Requirements", "## Implementation plans", "## Research"} {
			if strings.HasPrefix(l, sec) {
				fmt.Println(" ", l)
			}
		}
	}

	demo.Title("memory across sessions")
	must(m.Remember(ctx, "decision", "Webhooks from Stripe must be idempotent: dedupe on event id"))
	for _, mem := range must(m.Recall(ctx, "how should we handle stripe webhooks", 1)) {
		fmt.Printf("[%s] %s (score %.2f)\n", mem.Type, mem.Content, mem.Score)
	}

	demo.Title("ADRs are never edited, they are superseded")
	must(docs.SupersedeADR(1, "Store money as Decimal with fixed scale"))
	list := must(docs.List(ciam.ADR))
	for _, d := range list {
		fmt.Printf("ADR-%03d %-11s %s\n", d.Number, d.Status, d.Title)
	}

	demo.Title("the same capabilities as an MCP server (what VS Code / Cursor talk to)")
	srv := ciam.Toolkit{M: m, Project: root}.MCPServer(nil)
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = srv.ServeStdio(cctx, sr, sw) }()
	client := must(mcp.Connect(cctx, mcp.NewPipeTransport(cw, cr)))
	infos := must(client.ListTools(cctx))
	var names []string
	for _, t := range infos {
		names = append(names, t.Name)
	}
	fmt.Println("tools:", strings.Join(names, ", "))
	out := must(client.CallTool(cctx, "ciam_route", map[string]any{"intent": "I need to implement refunds"}))
	fmt.Println("ciam_route says:", strings.ReplaceAll(strings.TrimSpace(out.(string)), "\n", " "))

	st := must(m.Status(ctx))
	fmt.Printf("\nsearches=%d cache_hits=%d tokens_served=%d, estimated tokens saved vs reading the files=%d\n", st.Searches, st.CacheHits, st.TokensServed, st.TokensSaved)
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func must0(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
