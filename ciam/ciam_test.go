package ciam_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/mcp"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func djangoProject(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "shop")
	write(t, root, "manage.py", "import os\n")
	write(t, root, "billing/apps.py", "class BillingConfig: pass\n")
	write(t, root, "billing/models.py", `from django.db import models

class Invoice(models.Model):
    """An invoice issued to a customer."""
    # the amount in cents
    total_cents = models.IntegerField()
    stripe_customer_id = models.CharField(max_length=64)
`)
	write(t, root, "billing/views.py", "class InvoiceViewSet(ViewSet):\n    def list(self, request):\n        return Response([])\n")
	write(t, root, "billing/tasks.py", "@shared_task\ndef send_receipt_email(invoice_id):\n    pass\n")
	write(t, root, "billing/test_invoice.py", "def test_total():\n    assert True\n")
	write(t, root, "node_modules/junk/index.js", "var x = 1")
	write(t, root, "migrations_skip/.hidden/x.py", "x=1")
	return root
}

func newManager() *ciam.Manager { return ciam.New(embeddings.Fake{}, nil) }

func TestIndexClassifiesAndIsIncremental(t *testing.T) {
	ctx := context.Background()
	root := djangoProject(t)
	m := newManager()
	r, err := m.Index(ctx, root, ciam.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != "django" || r.Project != "shop" || r.Changed != r.Files || r.Chunks == 0 {
		t.Fatalf("%+v", r)
	}
	st, _ := m.Status(ctx)
	want := map[string]bool{"model": true, "view": true, "task": true, "test": true}
	for k := range want {
		if st.Projects[0].Types[k] == 0 {
			t.Errorf("chunk type %q missing: %v", k, st.Projects[0].Types)
		}
	}
	if st.Projects[0].Types["generic"] != 0 && st.Projects[0].Files < 5 {
		t.Logf("types: %v", st.Projects[0].Types)
	}
	// node_modules and hidden dirs are never indexed
	hits, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "var x", Limit: 20})
	for _, h := range hits {
		if strings.Contains(h.File, "node_modules") || strings.Contains(h.File, ".hidden") {
			t.Fatalf("indexed %s", h.File)
		}
	}

	// second run: nothing changed, nothing embedded
	r2, _ := m.Index(ctx, root, ciam.IndexOptions{})
	if r2.Changed != 0 || r2.Unchanged != r.Files || r2.Chunks != r.Chunks {
		t.Fatalf("not incremental: %+v", r2)
	}
	// change one file, delete another
	write(t, root, "billing/tasks.py", "@shared_task\ndef send_invoice_pdf(invoice_id):\n    pass\n")
	_ = os.Remove(filepath.Join(root, "billing/test_invoice.py"))
	r3, _ := m.Index(ctx, root, ciam.IndexOptions{})
	if r3.Changed != 1 || r3.Removed != 1 {
		t.Fatalf("%+v", r3)
	}
	res, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "send_invoice_pdf", Limit: 1})
	if len(res) != 1 || !strings.Contains(res[0].Content, "send_invoice_pdf") {
		t.Fatalf("stale index: %+v", res)
	}
	if old, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "send_receipt_email", Types: []string{"task"}, Limit: 5}); len(old) > 0 && strings.Contains(old[0].Content, "send_receipt_email") {
		t.Fatal("old chunk of a changed file still searchable")
	}
}

func TestHybridSearchAndFilters(t *testing.T) {
	ctx := context.Background()
	m := newManager()
	if _, err := m.Index(ctx, djangoProject(t), ciam.IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	// exact identifier: BM25 splits snake_case, so "stripe customer" finds stripe_customer_id
	res, err := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "stripe customer id", Limit: 3})
	if err != nil || len(res) == 0 || res[0].File != "billing/models.py" {
		t.Fatalf("%+v %v", res, err)
	}
	if res[0].BM25 <= 0 || res[0].Score <= 0 {
		t.Fatalf("scores %+v", res[0])
	}
	// type filter
	only, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "invoice", Types: []string{"task"}, Limit: 10})
	for _, r := range only {
		if r.Type != "task" {
			t.Fatalf("filter leaked %s", r.Type)
		}
	}
	// compression drops docstrings/comments but keeps code
	c, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "Invoice total_cents", Types: []string{"model"}, Limit: 1, Compress: true})
	if len(c) != 1 || strings.Contains(c[0].Content, "an invoice issued") || strings.Contains(c[0].Content, "the amount in cents") || !strings.Contains(c[0].Content, "total_cents") {
		t.Fatalf("%+v", c)
	}
	// repeated search is served from cache
	_, _ = m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "stripe customer id", Limit: 3})
	st, _ := m.Status(ctx)
	if st.CacheHits != 1 || st.Searches < 4 || st.TokensServed == 0 {
		t.Fatalf("%+v", st)
	}
	if _, err := m.Search(ctx, ciam.SearchRequest{Project: "shop"}); err == nil {
		t.Fatal("empty query must fail")
	}
	if none, err := m.Search(ctx, ciam.SearchRequest{Project: "ghost", Query: "x"}); err != nil || none != nil {
		t.Fatalf("unknown project: %v %v", none, err)
	}
}

func TestDocsLifecycleAndDecisionContext(t *testing.T) {
	ctx := context.Background()
	root := djangoProject(t)
	d := ciam.NewDocs(root)
	adr, err := d.New(ciam.ADR, `Use UUID as "primary key"`, "")
	if err != nil || filepath.Base(adr) != "ADR-001-use-uuid-as-primary-key.md" {
		t.Fatalf("%s %v", adr, err)
	}
	if _, err := d.New(ciam.ADR, "Second", ""); err != nil {
		t.Fatal(err)
	}
	prd, _ := d.New(ciam.PRD, "Billing via Stripe", "")
	plan, err := d.New(ciam.Plan, "Stripe integration", "PRD-001")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(plan)
	if !strings.Contains(string(body), `prd: "PRD-001"`) {
		t.Fatalf("plan not linked to PRD:\n%s", body)
	}
	if _, err := d.New(ciam.ADR, "  ", ""); err == nil {
		t.Fatal("empty title must fail")
	}

	// supersede keeps history: old = superseded, new references it
	newer, err := d.SupersedeADR(1, "Use ULID as primary key")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := d.List(ciam.ADR)
	if len(list) != 3 || list[0].Status != "superseded" || list[2].Number != 3 || list[0].Title != "Use UUID as 'primary key'" {
		t.Fatalf("%+v", list)
	}
	nb, _ := os.ReadFile(newer)
	if !strings.Contains(string(nb), `supersedes: "ADR-001"`) {
		t.Fatalf("missing supersedes:\n%s", nb)
	}
	if _, err := d.SupersedeADR(99, "x"); err == nil {
		t.Fatal("unknown ADR must fail")
	}
	if err := d.SetStatus(ciam.PRD, 1, "accepted"); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.List(ciam.PRD); p[0].Status != "accepted" {
		t.Fatalf("%+v (%s)", p, prd)
	}
	ex, err := d.PlanExcerpt(1, "Phase 2")
	if err != nil || !strings.Contains(ex, "Phase 2") || strings.Contains(ex, "Phase 1 -") {
		t.Fatalf("%q %v", ex, err)
	}

	// docs get their own chunk types and show up in decision context
	m := newManager()
	if _, err := m.Index(ctx, root, ciam.IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	dc, err := m.DecisionContext(ctx, "shop", "primary key ULID", 2)
	if err != nil || !strings.Contains(dc, "Architecture decisions") || !strings.Contains(dc, "ADR-003") {
		t.Fatalf("%v\n%s", err, dc)
	}
	// the Code section is code: documents never leak into it
	codeSection := dc[strings.Index(dc, "## Code"):strings.Index(dc, "## Architecture")]
	if strings.Contains(codeSection, "docs/adr") || strings.Contains(codeSection, "docs/prd") {
		t.Fatalf("documents leaked into the code section:\n%s", codeSection)
	}
	adrOnly, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "primary key", Types: []string{"adr"}})
	if len(adrOnly) == 0 || adrOnly[0].Type != "adr" {
		t.Fatalf("%+v", adrOnly)
	}
	skip, _ := newManager().Index(ctx, root, ciam.IndexOptions{SkipDocs: true})
	if st, _ := newManager().Status(ctx); st.Memories != 0 || skip.Files == 0 {
		t.Fatal("skip docs")
	}
}

func TestMemoryAndFileStorePersistence(t *testing.T) {
	ctx := context.Background()
	root := djangoProject(t)
	path := filepath.Join(t.TempDir(), "ciam.json")
	fs, err := ciam.OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	m := ciam.New(embeddings.Fake{}, fs)
	if _, err := m.Index(ctx, root, ciam.IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember(ctx, "decision", "we chose UUID primary keys for every model"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember(ctx, "", "deploys happen on tuesdays"); err != nil {
		t.Fatal(err)
	}

	// a new process: reopen the file, everything is still there
	fs2, err := ciam.OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	m2 := ciam.New(embeddings.Fake{}, fs2)
	res, err := m2.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "stripe customer id", Limit: 1})
	if err != nil || len(res) != 1 || res[0].File != "billing/models.py" {
		t.Fatalf("after reload: %+v %v", res, err)
	}
	mem, err := m2.Recall(ctx, "which primary keys", 1)
	if err != nil || len(mem) != 1 || mem[0].Type != "decision" || !strings.Contains(mem[0].Content, "UUID") {
		t.Fatalf("%+v %v", mem, err)
	}
	if r, _ := m2.Index(ctx, root, ciam.IndexOptions{}); r.Changed != 0 {
		t.Fatalf("hashes lost on reload: %+v", r)
	}
	if err := m2.ForgetMemory(ctx, mem[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ciam.OpenFileStore(path); err == nil {
		t.Fatal("corrupt store file must be an error, not silently empty")
	}
}

func TestToolkitOverMCP(t *testing.T) {
	root := djangoProject(t)
	m := newManager()
	var calls []string
	srv := ciam.Toolkit{M: m, Project: root}.MCPServer(func(name string, _ map[string]any, _ time.Duration, err error) {
		calls = append(calls, name)
	})
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ServeStdio(ctx, sr, sw) }()
	c, err := mcp.Connect(ctx, mcp.NewPipeTransport(cw, cr))
	if err != nil {
		t.Fatal(err)
	}
	infos, _ := c.ListTools(ctx)
	names := map[string]bool{}
	for _, i := range infos {
		names[i.Name] = true
	}
	for _, want := range []string{"ciam_index", "ciam_search", "ciam_context", "ciam_decision_context", "ciam_adr_search", "ciam_doc_new", "ciam_route", "ciam_remember", "ciam_recall", "ciam_compress", "ciam_project_map"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
	if names["ciam_draft"] {
		t.Error("ciam_draft must need a model")
	}
	if out, err := c.CallTool(ctx, "ciam_index", map[string]any{}); err != nil || !strings.Contains(out.(string), "django") {
		t.Fatalf("%v %v", out, err)
	}
	if out, _ := c.CallTool(ctx, "ciam_context", map[string]any{"query": "send receipt email task"}); !strings.Contains(out.(string), "send_receipt_email") {
		t.Fatalf("%v", out)
	}
	if out, _ := c.CallTool(ctx, "ciam_doc_new", map[string]any{"kind": "adr", "title": "Fix race in worker"}); !strings.Contains(out.(string), "ADR-001") {
		t.Fatalf("%v", out)
	}
	if _, err := c.CallTool(ctx, "ciam_doc_new", map[string]any{"kind": "bogus", "title": "x"}); err == nil {
		t.Fatal("bad kind must error")
	}
	if out, _ := c.CallTool(ctx, "ciam_route", map[string]any{"intent": "I need to implement billing"}); !strings.Contains(out.(string), "ciam_decision_context") {
		t.Fatalf("%v", out)
	}
	if out, _ := c.CallTool(ctx, "ciam_project_map", map[string]any{}); !strings.Contains(out.(string), "app `billing`") {
		t.Fatalf("%v", out)
	}
	if len(calls) < 5 {
		t.Fatalf("OnCall hook not invoked: %v", calls)
	}
	// without a default project the tool says so instead of guessing
	bare := ciam.Toolkit{M: m}.All()[1]
	if _, err := bare.Call(ctx, map[string]any{}); err == nil {
		t.Fatal("missing project must be an error")
	}
}

func TestDraftNeedsModelAndUsesPlan(t *testing.T) {
	ctx := context.Background()
	root := djangoProject(t)
	m := newManager()
	if _, err := m.Draft(ctx, ciam.DraftRequest{Root: root, Intent: "x"}); err == nil {
		t.Fatal("draft without a model must fail")
	}
	d := ciam.NewDocs(root)
	_, _ = d.New(ciam.Plan, "Receipts", "")
	_, _ = m.Index(ctx, root, ciam.IndexOptions{})
	fake := models.NewFake(messages.AI("def send(): ..."))
	m.Model = fake
	out, err := m.Draft(ctx, ciam.DraftRequest{Root: root, Intent: "email the receipt", PlanID: 1, Phase: "Phase 1"})
	if err != nil || !strings.Contains(out, "def send()") || !strings.Contains(out, "speculative") {
		t.Fatalf("%q %v", out, err)
	}
	prompt := fake.Calls[0][0].Text()
	if !strings.Contains(prompt, "Phase 1") || !strings.Contains(prompt, "<context>") || !strings.Contains(prompt, "email the receipt") {
		t.Fatalf("prompt lacks grounding:\n%s", prompt)
	}
	if _, err := m.Draft(ctx, ciam.DraftRequest{Root: root, Intent: "x", PlanID: 42}); err == nil {
		t.Fatal("unknown plan must fail")
	}
}

func TestCompressAndTokenize(t *testing.T) {
	py := "#!/usr/bin/env python\n# comment\n\"\"\"module doc\nmore\"\"\"\ndef f():\n    \"\"\"one liner\"\"\"\n    return 1\n\n\n\nx = 2\n"
	got := ciam.Compress(py, ".py")
	if strings.Contains(got, "comment") || strings.Contains(got, "module doc") || strings.Contains(got, "one liner") || !strings.Contains(got, "#!/usr/bin/env") || !strings.Contains(got, "return 1") || strings.Contains(got, "\n\n\n") {
		t.Fatalf("%q", got)
	}
	gosrc := "// Package x does y.\npackage x\n\n/* block\ncomment */\n//go:generate foo\nfunc A() {} // trailing stays\n"
	g := ciam.Compress(gosrc, ".go")
	if strings.Contains(g, "Package x") || strings.Contains(g, "block") || !strings.Contains(g, "//go:generate") || !strings.Contains(g, "func A()") {
		t.Fatalf("%q", g)
	}
	toks := strings.Join(ciam.Tokenize("LeadCaptureForm snake_case_name HTTPServer"), " ")
	for _, w := range []string{"leadcaptureform", "lead", "capture", "form", "snake", "case", "name"} {
		if !strings.Contains(toks, w) {
			t.Errorf("%q missing from %q", w, toks)
		}
	}
}

func TestDetectTypeAndProjectMap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module x\n")
	write(t, root, "cmd/app/main.go", "package main\nfunc main() {}\n")
	write(t, root, "internal/store/store.go", "package store\n")
	if ciam.DetectType(root) != "go" {
		t.Fatal("go.mod => go")
	}
	mp, err := ciam.ProjectMap(root)
	if err != nil || !strings.Contains(mp, "`cmd/app` package main") || !strings.Contains(mp, "package store") {
		t.Fatalf("%v\n%s", err, mp)
	}
	g := ciam.GenericClassifier("x/foo_test.go", "")
	if g != "test" {
		t.Fatalf("%q", g)
	}
}
