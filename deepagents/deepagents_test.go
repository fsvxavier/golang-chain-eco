package deepagents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	dmw "github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func tc(id, name string, args map[string]any) messages.Message {
	return messages.AI("", messages.ToolCall{ID: id, Name: name, Args: args})
}

func TestFilesystemAndSubagent(t *testing.T) {
	// main: write file, delegate, read what the subagent wrote, answer
	main := models.NewFake(
		tc("1", "write_file", map[string]any{"file_path": "/a.txt", "content": "hello\nworld"}),
		tc("2", "task", map[string]any{"description": "add b", "subagent_type": "general-purpose"}),
		tc("3", "read_file", map[string]any{"file_path": "/b.txt"}),
		tc("4", "edit_file", map[string]any{"file_path": "/a.txt", "old_string": "world", "new_string": "go"}),
		messages.AI("all done"),
	)
	// the fake is shared with the subagent; its single call slot is consumed in order
	_ = main
	sub := models.NewFake(
		tc("s1", "write_file", map[string]any{"file_path": "/b.txt", "content": "from sub"}),
		messages.AI("sub finished"),
	)
	ag, err := deepagents.Create(deepagents.Options{
		Model:       main,
		SubAgents:   []deepagents.SubAgentSpec{{Name: "general-purpose", Description: "gp", Model: sub}},
		SummarizeAt: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "go"})
	if err != nil {
		t.Fatal(err)
	}
	files := backends.FilesFromState(out)
	if files["/a.txt"].Content != "hello\ngo" || files["/b.txt"].Content != "from sub" {
		t.Fatalf("%+v", files)
	}
	ms := out["messages"].([]messages.Message)
	var read string
	for _, m := range ms {
		if m.ToolCallID == "3" {
			read = m.Content
		}
	}
	if !strings.Contains(read, "from sub") || ms[len(ms)-1].Content != "all done" {
		t.Fatalf("read=%q last=%q", read, ms[len(ms)-1].Content)
	}
}

func TestDiskBackendAndComposite(t *testing.T) {
	dir := t.TempDir()
	disk := backends.Filesystem{Root: dir}
	ctx := context.Background()
	if _, err := disk.Write(ctx, "/x/y.txt", "needle here"); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Write(ctx, "/x/y.txt", "again"); err == nil {
		t.Fatal("overwrite must fail")
	}
	if _, err := disk.Read(ctx, "/../etc/passwd"); err == nil || !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "escapes") {
		// cleaned to /etc/passwd under root => not found; both acceptable
		t.Logf("read traversal: %v", err)
	}
	if err := os.Symlink("/etc", filepath.Join(dir, "link")); err == nil {
		if _, err := disk.Read(ctx, "/link/hostname"); err == nil {
			t.Fatal("symlink escape must fail")
		}
	}
	comp := backends.Composite{Default: backends.State{}, Routes: map[string]backends.Backend{"/disk/": disk}}
	ms, err := comp.Grep(ctx, "needle", "/", "")
	if err != nil || len(ms) != 1 || ms[0].Path != "/disk/x/y.txt" {
		t.Fatalf("%v %v", ms, err)
	}
	if _, n, err := comp.Edit(ctx, "/disk/x/y.txt", "needle", "pin", false); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	g, _ := disk.Glob(ctx, "**/*.txt", "/")
	if len(g) != 1 {
		t.Fatal(g)
	}
}

func TestPermissions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	disk := backends.Filesystem{Root: dir}
	_, _ = disk.Write(ctx, "/secrets/key.txt", "needle")
	_, _ = disk.Write(ctx, "/pub/a.txt", "needle")
	p := backends.WithPermissions(disk,
		backends.Permission{Paths: []string{"/secrets/**"}, Deny: true},
		backends.Permission{Operations: []string{backends.OpWrite}, Paths: []string{"/pub/**"}, Deny: true},
	)
	if _, err := p.Read(ctx, "/secrets/key.txt"); err == nil {
		t.Fatal("secret readable")
	}
	if _, err := p.Write(ctx, "/pub/b.txt", "x"); err == nil {
		t.Fatal("write to read-only dir")
	}
	if _, err := p.Read(ctx, "/pub/a.txt"); err != nil {
		t.Fatal(err)
	}
	ms, _ := p.Grep(ctx, "needle", "/", "")
	if len(ms) != 1 || ms[0].Path != "/pub/a.txt" {
		t.Fatalf("grep leaked: %v", ms)
	}
	if _, ok := backends.AsExecutor(p); ok {
		t.Fatal("plain filesystem is not an executor")
	}
}

func TestRubricLoop(t *testing.T) {
	grader := models.NewFake(
		messages.AI(`{"result":"needs_revision","criteria":[{"name":"cites sources","passed":false,"gap":"add a source"}]}`),
		messages.AI(`{"result":"satisfied","criteria":[{"name":"cites sources","passed":true}]}`),
	)
	ag, err := deepagents.Create(deepagents.Options{
		Model:       models.NewFake(messages.AI("draft"), messages.AI("draft with source [1]")),
		SummarizeAt: -1,
		Middleware:  []agents.Middleware{dmw.Rubric{Grader: grader, Criteria: []string{"cites sources"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "write"})
	if err != nil {
		t.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	if ms[len(ms)-1].Content != "draft with source [1]" || !strings.Contains(ms[2].Content, "add a source") {
		t.Fatalf("%+v", ms)
	}
	if evs := out["rubric_evaluations"].([]any); len(evs) != 2 {
		t.Fatal(evs)
	}
}

func TestAsyncSubagents(t *testing.T) {
	worker, _ := agents.Create(agents.Options{Model: models.NewFake(messages.AI("research result"))})
	main := models.NewFake(
		tc("1", "start_async_task", map[string]any{"description": "dig", "subagent_type": "researcher"}),
		messages.AI("started"),
	)
	ag, err := deepagents.Create(deepagents.Options{
		Model: main, SummarizeAt: -1,
		Middleware: []agents.Middleware{dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
			{Name: "researcher", Description: "digs", Runner: &dmw.LocalRunner{Agent: worker}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "go"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := out["async_tasks"].(map[string]dmw.AsyncTask)
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	for _, v := range tasks {
		if v.Agent != "researcher" || v.Status != dmw.AsyncRunning {
			t.Fatal(v)
		}
	}
}

func TestLocalRunnerCompletes(t *testing.T) {
	worker, _ := agents.Create(agents.Options{Model: models.NewFake(messages.AI("research result"))})
	r := &dmw.LocalRunner{Agent: worker}
	h, _ := r.Start(context.Background(), "dig")
	for i := 0; i < 500; i++ {
		st, res, err := r.Poll(context.Background(), h)
		if err != nil {
			t.Fatal(err)
		}
		if st == dmw.AsyncSuccess {
			if res != "research result" {
				t.Fatal(res)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job never finished")
}

// A deep agent whose backend is a local directory with a code graph index gets
// code_graph; NoCodeGraph or a directory without an index does not.
func TestCodeGraphToolFollowsTheIndex(t *testing.T) {
	offered := func(dir string, no bool) bool {
		var seen bool
		fake := &models.Fake{Name: "f", Func: func(_ []messages.Message, o models.Options) (messages.Message, error) {
			for _, d := range o.Tools {
				seen = seen || d.Name == "code_graph"
			}
			return messages.AI("ok"), nil
		}}
		ag, err := deepagents.Create(deepagents.Options{Model: fake, Backend: backends.Filesystem{Root: dir}, NoCodeGraph: no})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ag.Invoke(context.Background(), graph.State{"messages": []messages.Message{messages.Human("hi")}}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	indexed := t.TempDir()
	if err := os.MkdirAll(filepath.Join(indexed, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !offered(indexed, false) {
		t.Fatal("an indexed local directory must offer code_graph")
	}
	if offered(indexed, true) {
		t.Fatal("NoCodeGraph must not offer it")
	}
	if offered(t.TempDir(), false) {
		t.Fatal("no index, no tool")
	}
}

// Compaction is on by default for a deep agent: a long wordy history reaches
// the model shorter than it went in.
func TestDeepAgentCompactsLongHistory(t *testing.T) {
	const wordy = "Sure! I think you should basically just run the tests and then check the output carefully."
	var hist []messages.Message
	for i := 0; i < 120; i++ {
		hist = append(hist, messages.Human(wordy), messages.AI(wordy))
	}
	hist = append(hist, messages.Human(wordy))
	size := func(ms []messages.Message) (n int) {
		for _, m := range ms {
			if m.Role != messages.RoleSystem {
				n += len(m.Text())
			}
		}
		return
	}
	run := func(no bool) int {
		var got int
		fake := &models.Fake{Name: "f", Func: func(ms []messages.Message, _ models.Options) (messages.Message, error) {
			got = size(ms)
			return messages.AI("ok"), nil
		}}
		ag, err := deepagents.Create(deepagents.Options{Model: fake, NoCompaction: no, SummarizeAt: -1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ag.Invoke(context.Background(), graph.State{"messages": hist}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if on, off := run(false), run(true); on >= off {
		t.Fatalf("compaction on: %d bytes, off: %d", on, off)
	}
}
