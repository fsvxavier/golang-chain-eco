package langmem_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func tc(name string, args map[string]any) messages.ToolCall {
	return messages.ToolCall{ID: messages.NewID(), Name: name, Args: args}
}

func note(s string) messages.ToolCall { return tc("Memory", map[string]any{"content": s}) }

// script returns a fake model replying with the given messages in order and
// recording the tool names offered on each call.
func script(rs ...messages.Message) *models.Fake { return models.NewFake(rs...) }

func conv(texts ...string) []messages.Message {
	var out []messages.Message
	for i, t := range texts {
		if i%2 == 0 {
			out = append(out, messages.Human(t))
		} else {
			out = append(out, messages.AI(t))
		}
	}
	return out
}

func ctxUser(id string) context.Context {
	return langmem.WithConfigurable(context.Background(), "langgraph_user_id", id)
}

func TestNamespace(t *testing.T) {
	ns, err := langmem.DefaultNamespace.Resolve(ctxUser("u1"))
	if err != nil || strings.Join(ns, "/") != "memories/u1" {
		t.Fatalf("%v %v", ns, err)
	}
	_, err = langmem.DefaultNamespace.Resolve(context.Background())
	if !errors.Is(err, langmem.ErrConfiguration) || !strings.Contains(err.Error(), "langgraph_user_id") {
		t.Fatalf("want configuration error, got %v", err)
	}
}

func TestIndexedStoreSemanticSearch(t *testing.T) {
	ctx := context.Background()
	ix := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
	ns := []string{"m"}
	_ = ix.Put(ctx, ns, "a", map[string]any{"content": "the user loves sushi and japanese food"})
	_ = ix.Put(ctx, ns, "b", map[string]any{"content": "the user works on kubernetes clusters"})
	hits, err := langmem.Query(ctx, ix, ns, "what japanese food does the user like", nil, 5)
	if err != nil || len(hits) != 2 || hits[0].Key != "a" || hits[0].Score <= hits[1].Score {
		t.Fatalf("%+v %v", hits, err)
	}
	if _, leaked := hits[0].Value["_embedding"]; leaked {
		t.Fatal("embedding leaked into results")
	}
	it, _ := ix.Get(ctx, ns, "a")
	if _, leaked := it.Value["_embedding"]; leaked {
		t.Fatal("embedding leaked via Get")
	}
}

func TestQueryLexicalFallback(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	_ = st.Put(ctx, []string{"m"}, "a", map[string]any{"content": "likes sushi"})
	_ = st.Put(ctx, []string{"m"}, "b", map[string]any{"content": "likes hiking"})
	hits, _ := langmem.Query(ctx, st, []string{"m"}, "sushi", nil, 5)
	if len(hits) != 1 || hits[0].Key != "a" {
		t.Fatalf("%+v", hits)
	}
	all, _ := langmem.Query(ctx, st, []string{"m"}, "", nil, 5)
	if len(all) != 2 {
		t.Fatal("empty query must list")
	}
}

func TestManagerInsertUpdateDeleteMultiStep(t *testing.T) {
	var offered [][]string
	step := 0
	var exID string
	m := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		var names []string
		for _, d := range o.Tools {
			names = append(names, d.Name)
		}
		offered = append(offered, names)
		step++
		switch step {
		case 1:
			return messages.AI("", note("likes tea"), tc("Episode", map[string]any{"observation": "o", "thoughts": "t", "action": "a", "result": "r"})), nil
		case 2:
			// ids appear in the "current memories" message
			last := msgs[len(msgs)-1].Text()
			i := strings.Index(last, "id=\"") + 4
			exID = last[i : i+strings.Index(last[i:], "\"")]
			return messages.AI("", tc("Memory", map[string]any{"content": "likes green tea", "json_doc_id": exID}), tc("Done", nil)), nil
		}
		return messages.AI("unexpected"), nil
	}}
	mgr := langmem.NewManager(m, langmem.WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema))
	out, err := mgr.Extract(context.Background(), conv("I like tea"), nil, 3)
	if err != nil || len(out) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	var got string
	for _, e := range out {
		if e.Kind == "Memory" {
			got = langmem.Text(e.Content)
			if e.ID != exID {
				t.Fatal("update must keep the id")
			}
		}
	}
	if got != "likes green tea" {
		t.Fatalf("got %q", got)
	}
	// step 1: schemas only (no RemoveDoc: deletes off, no Done); step 2 adds Done
	if strings.Join(offered[0], ",") != "Memory,Episode" || !strings.Contains(strings.Join(offered[1], ","), "Done") {
		t.Fatalf("offered tools: %v", offered)
	}
}

func TestManagerPermissions(t *testing.T) {
	existing := []langmem.Extracted{langmem.Note("e1", "old")}
	run := func(opts ...langmem.Option) []langmem.Extracted {
		m := script(messages.AI("",
			tc("Memory", map[string]any{"content": "new", "json_doc_id": "e1"}),
			note("brand new"),
			tc("RemoveDoc", map[string]any{"json_doc_id": "e1"})))
		out, err := langmem.NewManager(m, opts...).Extract(context.Background(), conv("x"), existing, 1)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// defaults: inserts+updates on, deletes off -> update applied, insert added, delete ignored
	out := run()
	if len(out) != 2 || langmem.Text(out[0].Content) != "new" || out[0].Deleted {
		t.Fatalf("%+v", out)
	}
	// profile mode: no inserts
	out = run(langmem.WithInserts(false))
	if len(out) != 1 || langmem.Text(out[0].Content) != "new" {
		t.Fatalf("%+v", out)
	}
	// deletes on, updates off: the update is rejected, the memory is then deleted
	out = run(langmem.WithUpdates(false), langmem.WithDeletes(true))
	if len(out) != 2 || !out[0].Deleted || out[0].ID != "e1" {
		t.Fatalf("%+v", out)
	}
}

func TestManagerRejectsInvalidContent(t *testing.T) {
	m := script(messages.AI("", tc("Memory", map[string]any{"wrong": 1})))
	out, err := langmem.NewManager(m).Extract(context.Background(), conv("x"), nil, 1)
	if err != nil || len(out) != 0 {
		t.Fatalf("invalid content must be dropped: %+v %v", out, err)
	}
}

func newSM(t *testing.T, m models.ChatModel, opts ...langmem.Option) (*langmem.StoreManager, *store.Memory) {
	st := store.NewMemory()
	opts = append([]langmem.Option{langmem.WithStore(st)}, opts...)
	return langmem.NewStoreManager(m, nil, opts...), st
}

func TestStoreManagerLifecycle(t *testing.T) {
	ctx := ctxUser("u1")
	var seen string
	step := 0
	m := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		step++
		seen = msgs[1].Text()
		if step == 1 {
			return messages.AI("", note("likes sushi"), note("lives in Rome")), nil
		}
		// find the id of "lives in Rome" and delete it, update nothing
		i := strings.Index(seen, "lives in Rome")
		j := strings.LastIndex(seen[:i], "id=\"") + 4
		id := seen[j : j+strings.Index(seen[j:], "\"")]
		return messages.AI("", tc("RemoveDoc", map[string]any{"json_doc_id": id})), nil
	}}
	sm, st := newSM(t, m)
	puts, err := sm.Process(ctx, conv("I like sushi, I live in Rome"))
	if err != nil || len(puts) != 2 {
		t.Fatalf("%+v %v", puts, err)
	}
	items, _ := st.Search(ctx, []string{"memories", "u1"}, nil, 0)
	if len(items) != 2 || items[0].Value["kind"] != "Memory" {
		t.Fatalf("%+v", items)
	}
	puts, err = sm.Process(ctx, conv("I moved away from Rome"))
	if err != nil || len(puts) != 0 {
		t.Fatalf("a delete writes nothing: %+v %v", puts, err)
	}
	if !strings.Contains(seen, "likes sushi") {
		t.Fatal("existing memories must be shown to the model")
	}
	items, _ = st.Search(ctx, []string{"memories", "u1"}, nil, 0)
	if len(items) != 1 || langmem.Text(items[0].Value["content"]) != "likes sushi" {
		t.Fatalf("after delete: %+v", items)
	}
	// other users are isolated
	other, _ := st.Search(ctx, []string{"memories", "u2"}, nil, 0)
	if len(other) != 0 {
		t.Fatal("namespace leak")
	}
}

type Profile struct {
	Name  string   `json:"name"`
	Likes []string `json:"likes"`
}

func TestStoreManagerProfileDefault(t *testing.T) {
	ctx := ctxUser("u1")
	m := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		// profile mode offers no RemoveDoc, and the seeded doc is visible
		for _, d := range o.Tools {
			if d.Name == "RemoveDoc" {
				t.Error("RemoveDoc must not be offered")
			}
		}
		if !strings.Contains(msgs[1].Text(), `"name":""`) {
			t.Errorf("seed not shown: %s", msgs[1].Text())
		}
		id := "default"
		return messages.AI("", tc("Profile", map[string]any{"name": "Lex", "likes": []any{"go"}, "json_doc_id": id})), nil
	}}
	sm, st := newSM(t, m,
		langmem.WithSchemas(langmem.SchemaFor[Profile]("Profile", "The user profile")),
		langmem.WithInserts(false), langmem.WithDeletes(false),
		langmem.WithDefault(langmem.Seed{Content: Profile{Likes: []string{}}}))
	if _, err := sm.Process(ctx, conv("I'm Lex and I like go")); err != nil {
		t.Fatal(err)
	}
	items, _ := st.Search(ctx, []string{"memories", "u1"}, nil, 0)
	if len(items) != 1 || items[0].Key != "default" {
		t.Fatalf("%+v", items)
	}
	p, err := langmem.Decode[Profile](items[0].Value["content"])
	if err != nil || p.Name != "Lex" || items[0].Value["kind"] != "Profile" {
		t.Fatalf("%+v %v", items[0].Value, err)
	}
}

func TestStoreManagerPhasesAndQueryModel(t *testing.T) {
	ctx := ctxUser("u1")
	var calls []string
	m := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		calls = append(calls, msgs[1].Text()[:20])
		if strings.Contains(msgs[1].Text(), "Deduplicate") {
			return messages.AI("", note("consolidated")), nil
		}
		return messages.AI("", note("a"), note("b")), nil
	}}
	q := &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
		return messages.AI("", tc("search_memory", map[string]any{"query": "anything"})), nil
	}}
	sm, st := newSM(t, m, langmem.WithQueryModel(q), langmem.WithPhases(langmem.Phase{}))
	puts, err := sm.Process(ctx, conv("hello"))
	if err != nil || len(puts) != 3 || len(q.Calls) != 1 || len(calls) != 2 {
		t.Fatalf("puts=%d qcalls=%d calls=%v err=%v", len(puts), len(q.Calls), calls, err)
	}
	items, _ := st.Search(ctx, []string{"memories", "u1"}, nil, 0)
	if len(items) != 3 {
		t.Fatalf("%d", len(items))
	}
}

func TestStoreManagerNoStore(t *testing.T) {
	sm := langmem.NewStoreManager(script(messages.AI("x")), nil)
	if _, err := sm.Process(ctxUser("u"), conv("x")); !errors.Is(err, langmem.ErrConfiguration) {
		t.Fatalf("%v", err)
	}
}

func TestMemoryTools(t *testing.T) {
	ctx := ctxUser("u1")
	st := store.NewMemory()
	cfg := langmem.ToolConfig{Store: st}
	manage, search := langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)

	out, err := manage.Call(ctx, map[string]any{"content": "prefers golang"})
	if err != nil || !strings.HasPrefix(out.(string), "created memory ") {
		t.Fatalf("%v %v", out, err)
	}
	id := strings.TrimPrefix(out.(string), "created memory ")
	if _, err := manage.Call(ctx, map[string]any{"content": "x", "id": id}); err == nil {
		t.Fatal("create with id must fail")
	}
	if _, err := manage.Call(ctx, map[string]any{"action": "delete"}); err == nil {
		t.Fatal("delete without id must fail")
	}
	if out, err = manage.Call(ctx, map[string]any{"action": "update", "id": id, "content": "prefers go and rust"}); err != nil || out != "updated memory "+id {
		t.Fatalf("%v %v", out, err)
	}
	res, err := search.Call(ctx, map[string]any{"query": "rust"})
	if err != nil || !strings.Contains(res.(string), "prefers go and rust") {
		t.Fatalf("%v %v", res, err)
	}
	if res, _ = search.Call(ctx, map[string]any{"query": "rust", "offset": 1}); res != "[]" {
		t.Fatalf("offset: %v", res)
	}
	if out, _ = manage.Call(ctx, map[string]any{"action": "delete", "id": id}); out != "Deleted memory "+id {
		t.Fatalf("%v", out)
	}
	// restricted actions are rejected
	ro := langmem.ManageMemory(langmem.ToolConfig{Store: st, Actions: []string{"create"}})
	if _, err := ro.Call(ctx, map[string]any{"action": "delete", "id": "x"}); err == nil {
		t.Fatal("delete must be forbidden")
	}
	if _, err := manage.Call(context.Background(), map[string]any{"content": "x"}); !errors.Is(err, langmem.ErrConfiguration) {
		t.Fatalf("missing user id: %v", err)
	}
}

func TestMemorySearcherAndThreadExtractor(t *testing.T) {
	ctx := ctxUser("u1")
	st := store.NewMemory()
	_ = st.Put(ctx, []string{"memories", "u1"}, "a", map[string]any{"content": "likes sushi"})
	_ = st.Put(ctx, []string{"memories", "u1"}, "b", map[string]any{"content": "has a dog named Rex"})
	m := script(messages.AI("", tc("search_memory", map[string]any{"query": "sushi"}), tc("search_memory", map[string]any{"query": "dog"})))
	hits, err := langmem.MemorySearcher{Model: m, Store: st}.Search(ctx, conv("tell me about me"))
	if err != nil || len(hits) != 2 {
		t.Fatalf("%+v %v", hits, err)
	}
	fm := script(messages.AI("", tc("ThreadSummary", map[string]any{"title": "Sushi", "summary": "talked sushi"})))
	ts, err := langmem.ExtractThread[langmem.ThreadSummary](ctx, fm, conv("hi", "hello"), "")
	if err != nil || ts.Title != "Sushi" {
		t.Fatalf("%+v %v", ts, err)
	}
}

func TestOptimizers(t *testing.T) {
	ctx := context.Background()
	tr := []langmem.Trajectory{{Messages: conv("explain inheritance", "theory..."), Feedback: map[string]any{"score": 0}}}
	p := langmem.Prompt{Name: "main", Prompt: "You are helpful for {topic}."}

	t.Run("gradient/no adjustment", func(t *testing.T) {
		m := script(messages.AI("", tc("think", map[string]any{"thought": "hm"})),
			messages.AI("", tc("recommend", map[string]any{"warrants_adjustment": false})))
		o := langmem.NewPromptOptimizer(m, langmem.Gradient, langmem.OptimizerConfig{})
		out, err := o.Optimize(ctx, tr, p)
		if err != nil || out != p.Prompt || len(m.Calls) != 2 {
			t.Fatalf("%q %v calls=%d", out, err, len(m.Calls))
		}
	})
	t.Run("gradient/rewrite", func(t *testing.T) {
		m := script(
			messages.AI("", tc("think", map[string]any{"thought": "hm"})),
			messages.AI("", tc("recommend", map[string]any{"warrants_adjustment": true, "hypotheses": "too theoretical", "full_recommendations": "add examples"})),
			messages.AI("", tc("OptimizedPromptOutput", map[string]any{"analysis": "a", "improved_prompt": "You are helpful for {topic}. Always show code {literal}."})))
		o := langmem.NewPromptOptimizer(m, langmem.Gradient, langmem.OptimizerConfig{})
		out, err := o.Optimize(ctx, tr, p)
		if err != nil || out != "You are helpful for {topic}. Always show code {{literal}}." {
			t.Fatalf("%q %v", out, err)
		}
		if !strings.Contains(m.Calls[2][0].Text(), "add examples") {
			t.Fatal("recommendations must reach the update step")
		}
	})
	t.Run("rewrite dropping a variable fails", func(t *testing.T) {
		m := script(messages.AI("", tc("OptimizedPromptOutput", map[string]any{"analysis": "a", "improved_prompt": "no variable here"})))
		o := langmem.NewPromptOptimizer(m, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 1})
		if _, err := o.Optimize(ctx, tr, p); err == nil || !strings.Contains(err.Error(), "topic") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("metaprompt/reflects then rewrites", func(t *testing.T) {
		m := script(messages.AI("", tc("critique", map[string]any{"criticism": "c"})),
			messages.AI("", tc("OptimizedPromptOutput", map[string]any{"analysis": "a", "improved_prompt": "better {topic}"})))
		o := langmem.NewPromptOptimizer(m, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 2})
		out, err := o.Optimize(ctx, tr, p)
		if err != nil || out != "better {topic}" {
			t.Fatalf("%q %v", out, err)
		}
	})
	t.Run("metaprompt/no recommendations", func(t *testing.T) {
		m := script(messages.AI("", tc("OptimizedPromptOutput", map[string]any{"analysis": "a", "improved_prompt": "No recommendations."})))
		out, _ := langmem.NewPromptOptimizer(m, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 1}).Optimize(ctx, tr, p)
		if out != p.Prompt {
			t.Fatalf("%q", out)
		}
	})
	t.Run("prompt_memory", func(t *testing.T) {
		m := script(messages.AI("", tc("GeneralResponse", map[string]any{"logic": "l", "update_prompt": true, "new_prompt": "v2 {topic}"})))
		out, err := langmem.NewPromptOptimizer(m, langmem.PromptMemory, langmem.OptimizerConfig{}).Optimize(ctx, tr, p)
		if err != nil || out != "v2 {topic}" || len(m.Calls) != 1 {
			t.Fatalf("%q %v", out, err)
		}
	})
	t.Run("no trajectories = no change, no model call", func(t *testing.T) {
		m := script()
		out, err := langmem.NewPromptOptimizer(m, langmem.Gradient, langmem.OptimizerConfig{}).Optimize(ctx, nil, p)
		if err != nil || out != p.Prompt || len(m.Calls) != 0 {
			t.Fatal(out, err)
		}
	})
}

func TestMultiPromptOptimizer(t *testing.T) {
	ctx := context.Background()
	m := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		switch o.Tools[0].Name {
		case "Classify":
			return messages.AI("", tc("Classify", map[string]any{"reasoning": "r", "which": []any{"summarize"}})), nil
		default: // prompt_memory update
			return messages.AI("", tc("GeneralResponse", map[string]any{"logic": "l", "update_prompt": true, "new_prompt": "Summarize briefly"})), nil
		}
	}}
	mo := &langmem.MultiPromptOptimizer{Model: m, Kind: langmem.PromptMemory}
	prompts := []langmem.Prompt{{Name: "research", Prompt: "Research"}, {Name: "summarize", Prompt: "Summarize"}}
	out, err := mo.Optimize(ctx, []langmem.Trajectory{{Messages: conv("q", "a"), Feedback: "too long"}}, prompts)
	if err != nil || out[0].Prompt != "Research" || out[1].Prompt != "Summarize briefly" {
		t.Fatalf("%+v %v", out, err)
	}
	// classifier naming an unknown prompt is an error
	m2 := script(messages.AI("", tc("Classify", map[string]any{"reasoning": "r", "which": []any{"nope"}})))
	if _, err := (&langmem.MultiPromptOptimizer{Model: m2}).Optimize(ctx, []langmem.Trajectory{{Messages: conv("q")}}, prompts); err == nil {
		t.Fatal("want error")
	}
}

func idMsgs(n int, size int) []messages.Message {
	var out []messages.Message
	for i := range n {
		m := messages.Human(fmt.Sprintf("m%02d %s", i, strings.Repeat("x", size)))
		if i%2 == 1 {
			m = messages.AI(m.Content)
		}
		m.ID = fmt.Sprintf("id%d", i)
		out = append(out, m)
	}
	return out
}

func TestSummarizeMessages(t *testing.T) {
	ctx := context.Background()
	sum := models.NewFake(messages.AI("SUMMARY-1"), messages.AI("SUMMARY-2"))
	opts := langmem.SummarizeOptions{Model: sum, MaxTokens: 100, MaxSummaryTokens: 20, Counter: func(ms []messages.Message) int { return len(ms) * 10 }}

	// under budget: untouched
	res, err := langmem.SummarizeMessages(ctx, idMsgs(5, 1), nil, opts)
	if err != nil || len(res.Messages) != 5 || res.Running != nil || len(sum.Calls) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	// 12 messages = 120 tokens >= 100: summarize the oldest until the rest fits 80
	msgs := idMsgs(12, 1)
	res, err = langmem.SummarizeMessages(ctx, msgs, nil, opts)
	if err != nil || res.Running == nil {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Messages[0].Name != "summary" || !strings.Contains(res.Messages[0].Text(), "SUMMARY-1") {
		t.Fatalf("%+v", res.Messages[0])
	}
	kept := len(res.Messages) - 1
	if kept > 8 || len(res.Running.SummarizedMessageIDs)+kept != 12 {
		t.Fatalf("kept=%d summarized=%d", kept, len(res.Running.SummarizedMessageIDs))
	}
	if res.Running.LastSummarizedMessageID != msgs[len(res.Running.SummarizedMessageIDs)-1].ID {
		t.Fatal("last id")
	}
	if !strings.Contains(sum.Calls[0][len(sum.Calls[0])-1].Text(), "Create a summary") {
		t.Fatal("initial prompt")
	}
	// incremental: the next call only reuses the summary (no new model call) while it still fits
	more := append(slices(msgs), idMsgs(14, 1)[12:]...)
	res2, err := langmem.SummarizeMessages(ctx, more, res.Running, opts)
	if err != nil || len(sum.Calls) != 1 || !strings.Contains(res2.Messages[0].Text(), "SUMMARY-1") {
		t.Fatalf("calls=%d %v", len(sum.Calls), err)
	}
	// once it overflows again the existing summary is extended, not restarted
	more = append(slices(msgs), idMsgs(22, 1)[12:]...)
	res3, err := langmem.SummarizeMessages(ctx, more, res.Running, opts)
	if err != nil || len(sum.Calls) != 2 || !strings.Contains(sum.Calls[1][len(sum.Calls[1])-1].Text(), "SUMMARY-1") {
		t.Fatalf("calls=%d %v", len(sum.Calls), err)
	}
	if len(res3.Running.SummarizedMessageIDs) <= len(res.Running.SummarizedMessageIDs) {
		t.Fatal("running summary must grow")
	}
	// validation
	if _, err := langmem.SummarizeMessages(ctx, msgs, nil, langmem.SummarizeOptions{Model: sum, MaxTokens: 10, MaxSummaryTokens: 10}); err == nil {
		t.Fatal("max_summary_tokens >= max_tokens must fail")
	}
	noID := []messages.Message{messages.Human("a")}
	if _, err := langmem.SummarizeMessages(ctx, noID, nil, opts); err == nil {
		t.Fatal("missing ids must fail")
	}
}

func slices(m []messages.Message) []messages.Message { return append([]messages.Message(nil), m...) }

func TestSummarizeKeepsToolResultsWithTheirCall(t *testing.T) {
	ctx := context.Background()
	msgs := idMsgs(10, 1)
	call := messages.AI("", messages.ToolCall{ID: "c1", Name: "t", Args: map[string]any{}})
	call.ID = "call"
	res := messages.Tool("r", "c1")
	res.ID = "res"
	msgs[4], msgs[5] = call, res
	sum := models.NewFake(messages.AI("S"))
	// the trigger lands exactly on the AI tool-call message: its result must be summarized too
	o := langmem.SummarizeOptions{Model: sum, MaxTokens: 50, MaxSummaryTokens: 10, MaxTokensBeforeSummary: 50, Counter: func(ms []messages.Message) int { return len(ms) * 10 }}
	r, err := langmem.SummarizeMessages(ctx, msgs, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	ids := strings.Join(r.Running.SummarizedMessageIDs, ",")
	if strings.Contains(ids, "call") && !strings.Contains(ids, "res") {
		t.Fatalf("tool result separated from its call: %s", ids)
	}
	for _, m := range r.Messages[1:] {
		if m.Role == messages.RoleTool {
			t.Fatal("kept window starts with orphan tool result")
		}
	}
}

func TestReflectionExecutorDebounce(t *testing.T) {
	var runs atomic.Int32
	var last atomic.Int32
	ex := &langmem.ReflectionExecutor{Run: func(_ context.Context, in langmem.Payload) ([]langmem.Put, error) {
		runs.Add(1)
		last.Store(int32(len(in.Messages)))
		return []langmem.Put{{Key: "k"}}, nil
	}}
	ctx := context.Background()
	f1, _ := ex.Submit(ctx, langmem.Payload{Messages: conv("a")}, langmem.SubmitOptions{After: 100 * time.Millisecond, ThreadID: "t"})
	f2, _ := ex.Submit(ctx, langmem.Payload{Messages: conv("a", "b", "c")}, langmem.SubmitOptions{After: 100 * time.Millisecond, ThreadID: "t"})
	if _, err := f1.Wait(); !errors.Is(err, langmem.ErrCancelled) {
		t.Fatalf("first run must be cancelled, got %v", err)
	}
	puts, err := f2.Wait()
	if err != nil || len(puts) != 1 || runs.Load() != 1 || last.Load() != 3 {
		t.Fatalf("runs=%d last=%d %v", runs.Load(), last.Load(), err)
	}
	// Shutdown flushes pending work immediately
	f3, _ := ex.Submit(ctx, langmem.Payload{Messages: conv("z")}, langmem.SubmitOptions{After: time.Hour, ThreadID: "u"})
	ex.Shutdown(true, false)
	if _, err := f3.Wait(); err != nil || runs.Load() != 2 {
		t.Fatalf("flush: %v runs=%d", err, runs.Load())
	}
	// cancel drops it
	f4, _ := ex.Submit(ctx, langmem.Payload{Messages: conv("z")}, langmem.SubmitOptions{After: time.Hour, ThreadID: "v"})
	ex.Shutdown(true, true)
	if _, err := f4.Wait(); !errors.Is(err, langmem.ErrCancelled) || runs.Load() != 2 {
		t.Fatalf("%v", err)
	}
}

func TestMiddlewareInjectToolsAndBackground(t *testing.T) {
	st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
	ctx := ctxUser("u1")
	_ = st.Put(ctx, []string{"memories", "u1"}, "m1", map[string]any{"content": "the user loves sushi"})

	var system string
	var offered []string
	agent := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		system = msgs[0].Text()
		for _, d := range o.Tools {
			offered = append(offered, d.Name)
		}
		return messages.AI("sushi it is"), nil
	}}
	extractor := script(messages.AI("", note("asked about food")))
	sm := langmem.NewStoreManager(extractor, nil, langmem.WithStore(st))
	ex := &langmem.ReflectionExecutor{Manager: sm}
	mw := langmem.Middleware{Store: st, Inject: true, ExposeTools: true, Manager: sm, Executor: ex}
	ag, err := agents.Create(agents.Options{Model: agent, SystemPrompt: "You are helpful.", Middleware: []agents.Middleware{mw}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ag.Invoke(ctx, graph.State{"messages": "what food does the user love?"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "the user loves sushi") || !strings.HasPrefix(system, "You are helpful.") {
		t.Fatalf("memories not injected: %q", system)
	}
	if strings.Join(offered, ",") != "manage_memory,search_memory" {
		t.Fatalf("%v", offered)
	}
	ex.Shutdown(true, false)
	items, _ := st.Search(ctx, []string{"memories", "u1"}, nil, 0)
	if len(items) != 2 {
		t.Fatalf("background formation did not write: %d", len(items))
	}
}

func TestSummarizationMiddlewareInAgent(t *testing.T) {
	var seen []messages.Message
	agent := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		seen = msgs
		return messages.AI("ok"), nil
	}}
	sum := models.NewFake(messages.AI("THE-SUMMARY"))
	mw := langmem.SummarizationMiddleware{SummarizeOptions: langmem.SummarizeOptions{
		Model: sum, MaxTokens: 60, MaxSummaryTokens: 10,
		Counter: func(ms []messages.Message) int { return len(ms) * 10 }}}
	ag, err := agents.Create(agents.Options{Model: agent, Middleware: []agents.Middleware{mw}})
	if err != nil {
		t.Fatal(err)
	}
	var in []messages.Message
	for i := range 9 {
		in = append(in, messages.Human(fmt.Sprintf("q%d", i)))
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": in})
	if err != nil {
		t.Fatal(err)
	}
	if len(out["messages"].([]messages.Message)) != 10 {
		t.Fatal("stored history must stay complete")
	}
	if len(seen) >= 9 || seen[0].Name != "summary" || !strings.Contains(seen[0].Text(), "THE-SUMMARY") {
		t.Fatalf("model must see summary+recent, got %d msgs: %+v", len(seen), seen[0])
	}
}
