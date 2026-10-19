package guards_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
)

func TestShellParsing(t *testing.T) {
	cases := []struct {
		in   string
		want []string // executable + args of each command, joined
	}{
		{`git status && echo "a b" ; ls -la | grep x`, []string{"git status", "echo a b", "ls -la", "grep x"}},
		{`FOO=1 BAR=2 sudo -n env X=1 gh pr view 5`, []string{"gh pr view 5"}},
		{`bash -c 'cd repo && gh pr create --fill'`, []string{"cd repo", "gh pr create --fill"}},
		{`sh -lc "echo hi; git push origin main"`, []string{"echo hi", "git push origin main"}},
		{`eval "gh pr create"`, []string{"gh pr create"}},
		{`echo 'it'\''s' # comment gh pr create`, []string{"echo it's"}},
		{`timeout 30 git push`, []string{"git push"}},
		{"git\\\n push", []string{"git push"}},
	}
	for _, c := range cases {
		cmds, _ := guards.Commands(c.in)
		var got []string
		for _, cm := range cmds {
			got = append(got, strings.Join(cm, " "))
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q\n got  %q\n want %q", c.in, got, c.want)
		}
	}
	if _, unsafe := guards.Commands("git push $(echo origin) main"); !unsafe {
		t.Error("command substitution must be reported")
	}
	if _, unsafe := guards.Commands("echo `date`"); !unsafe {
		t.Error("backticks must be reported")
	}
	// depth limit: arbitrarily nested shells do not hang
	nested := "echo x"
	for range 10 {
		nested = "bash -c '" + strings.ReplaceAll(nested, "'", `'\''`) + "'"
	}
	if cmds, _ := guards.Commands(nested); len(cmds) == 0 {
		t.Error("nested shells must still return something")
	}
}

func TestPRCreationDetection(t *testing.T) {
	block := []string{
		`gh pr create --title x --body y`,
		`gh pr new`,
		`cd repo && gh pr create`,
		`bash -c "gh pr create --fill"`,
		`GH_TOKEN=abc gh pr create`,
		`gh -R o/r pr create`,
		`gh api repos/o/r/pulls -f title=x -f head=feat -f base=main`,
		`gh api -X POST /repos/o/r/pulls --input pr.json`,
		`gh api --method POST repos/o/r/pulls`,
		`curl -X POST -H "Authorization: token $T" https://api.github.com/repos/o/r/pulls -d '{"title":"x"}'`,
		`curl --data @pr.json https://api.github.com/repos/o/r/pulls`,
		`hub pull-request -m x`,
		`echo ok && gh pr create`,
		`$(echo gh) pr create`,
	}
	for _, c := range block {
		if !guards.CreatesPullRequest(c) {
			t.Errorf("must block: %s", c)
		}
	}
	allow := []string{
		`gh pr view 5`,
		`gh pr edit 5 --body x`,
		`gh pr checks 5 --watch`,
		`gh pr list`,
		`gh api repos/o/r/pulls/5/comments`,
		`gh api repos/o/r/pulls`, // a GET
		`gh api -X GET repos/o/r/pulls -f state=open`,
		`curl https://api.github.com/repos/o/r/pulls`,
		`curl -s https://api.github.com/repos/o/r/pulls/5`,
		`git push origin feat`,
		`echo "gh pr create is forbidden"`,
		`grep -rn "pr create" docs/`,
	}
	for _, c := range allow {
		if guards.CreatesPullRequest(c) {
			t.Errorf("must allow: %s", c)
		}
	}
}

func shellCall(cmd string) messages.ToolCall {
	return messages.ToolCall{ID: "c1", Name: "execute", Args: map[string]any{"command": cmd}}
}

func TestPRCreationGuardBlocksTheTool(t *testing.T) {
	ran := 0
	next := func(context.Context, messages.ToolCall) (messages.Message, error) {
		ran++
		return messages.Tool("ok", "c1"), nil
	}
	g := guards.PRCreationGuard{}
	out, err := g.WrapToolCall(context.Background(), shellCall("gh pr create --fill"), next)
	if err != nil || ran != 0 || out.Status != "error" || !strings.Contains(out.Text(), "open_pull_request") || out.ToolCallID != "c1" {
		t.Fatalf("%+v %v ran=%d", out, err, ran)
	}
	if _, err := g.WrapToolCall(context.Background(), shellCall("go test ./..."), next); err != nil || ran != 1 {
		t.Fatal("ordinary commands pass")
	}
	// other tools and the monitor's second argument
	other := messages.ToolCall{ID: "c2", Name: "write_file", Args: map[string]any{"command": "gh pr create"}}
	if _, _ = g.WrapToolCall(context.Background(), other, next); ran != 2 {
		t.Fatal("only shell tools are inspected")
	}
	mon := messages.ToolCall{ID: "c3", Name: "monitor", Args: map[string]any{"command": "tail -f log", "filter": "gh pr create"}}
	if out, _ := g.WrapToolCall(context.Background(), mon, next); out.Status != "error" {
		t.Fatal("monitor filter is a second shell command")
	}
}

// ------------------------------------------------------ workflow push guard

func gitRepo(t *testing.T) (string, backends.Executor) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	up := filepath.Join(root, "up.git")
	work := filepath.Join(root, "work")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	_ = os.MkdirAll(up, 0o755)
	run(up, "init", "-q", "--bare", "-b", "main")
	_ = os.MkdirAll(work, 0o755)
	run(work, "init", "-q", "-b", "main")
	run(work, "remote", "add", "origin", up)
	_ = os.WriteFile(filepath.Join(work, "a.txt"), []byte("a"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "-q", "-m", "base")
	run(work, "push", "-q", "-u", "origin", "main")
	run(work, "checkout", "-q", "-b", "feat")
	be := backends.LocalShell{Filesystem: backends.Filesystem{Root: work}}
	return work, be
}

func commit(t *testing.T, work string, files map[string]string, msg string) {
	for p, c := range files {
		full := filepath.Join(work, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(c), 0o644)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
}

func TestParsePushes(t *testing.T) {
	cases := []struct {
		in            string
		remote        string
		refs          []string
		dir           string
		force, unsafe bool
		count         int
	}{
		{"git push origin feat", "origin", []string{"feat"}, "", false, false, 1},
		{"git push", "", nil, "", false, false, 1},
		{"git -C /repo push -u origin HEAD:feat", "origin", []string{"HEAD:feat"}, "/repo", false, false, 1},
		{"cd /w/repo && git push --force-with-lease origin +feat:feat", "origin", []string{"+feat:feat"}, "/w/repo", true, false, 1},
		{"git push -o ci.skip origin feat", "origin", []string{"feat"}, "", false, false, 1},
		{"git status && git pull", "", nil, "", false, false, 0},
		{"git push origin $(git branch --show-current)", "", nil, "", false, true, 1},
	}
	for _, c := range cases {
		ps, unsafe := guards.ParsePushes(c.in)
		if len(ps) != c.count || unsafe != c.unsafe {
			t.Errorf("%q: %+v unsafe=%v", c.in, ps, unsafe)
			continue
		}
		if c.count == 1 && !c.unsafe {
			p := ps[0]
			if p.Remote != c.remote || strings.Join(p.Refspecs, ",") != strings.Join(c.refs, ",") || p.Dir != c.dir || p.Force != c.force {
				t.Errorf("%q: %+v", c.in, p)
			}
		}
	}
}

func TestWorkflowPushNeedsApprovalBoundToTheDiff(t *testing.T) {
	work, ex := gitRepo(t)
	ctx := context.Background()
	ap := &guards.MemoryApprover{}
	g := guards.WorkflowPushGuard{Exec: ex, Approver: ap}
	ran := 0
	next := func(context.Context, messages.ToolCall) (messages.Message, error) {
		ran++
		return messages.Tool("pushed", "c1"), nil
	}

	// a normal code push goes through
	commit(t, work, map[string]string{"src/x.go": "package x"}, "code")
	if out, err := g.WrapToolCall(ctx, shellCall("git push origin feat"), next); err != nil || ran != 1 || out.Status == "error" {
		t.Fatalf("%+v %v", out, err)
	}
	// a workflow change is held back
	commit(t, work, map[string]string{".github/workflows/ci.yml": "on: push\njobs: {}\n"}, "ci")
	out, err := g.WrapToolCall(ctx, shellCall("git push origin feat"), next)
	if err != nil || ran != 1 || out.Status != "error" || !strings.Contains(out.Text(), ".github/workflows/ci.yml") || !strings.Contains(out.Text(), "Nothing ran") {
		t.Fatalf("%+v %v ran=%d", out, err, ran)
	}
	if len(ap.Requested) != 1 || ap.Requested[0].Branch != "feat" || !strings.Contains(ap.Requested[0].DiffPreview, "on: push") {
		t.Fatalf("%+v", ap.Requested)
	}
	id := ap.Requested[0].ID
	// asking again does not spam: same id
	_, _ = g.WrapToolCall(ctx, shellCall("git push origin feat"), next)
	if len(ap.Requested) != 1 {
		t.Fatal("approval requests must be idempotent per id")
	}
	// approval allows exactly that content
	ap.Approve(id)
	if out, _ := g.WrapToolCall(ctx, shellCall("git push origin feat"), next); out.Status == "error" || ran != 2 {
		t.Fatalf("approved push must run: %+v", out)
	}
	// ...but not a later, different workflow change
	commit(t, work, map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  evil: {}\n"}, "ci again")
	if out, _ := g.WrapToolCall(ctx, shellCall("git push origin feat"), next); out.Status != "error" || ran != 2 {
		t.Fatalf("a changed diff needs a new approval: %+v", out)
	}
	// a substitution next to a push cannot be inspected: blocked
	if out, _ := g.WrapToolCall(ctx, shellCall("git push origin $(git branch --show-current)"), next); out.Status != "error" || !strings.Contains(out.Text(), "cannot be inspected") {
		t.Fatalf("%+v", out)
	}
	// an odd refspec is blocked rather than guessed at
	if out, _ := g.WrapToolCall(ctx, shellCall("git push origin 'feat;rm -rf /'"), next); out.Status != "error" {
		t.Fatalf("%+v", out)
	}
	// non-push git and other commands are untouched
	if _, err := g.WrapToolCall(ctx, shellCall("git status"), next); err != nil || ran != 3 {
		t.Fatalf("ran=%d %v", ran, err)
	}
	// no approver configured: workflow pushes stay blocked
	strict := guards.WorkflowPushGuard{Exec: ex}
	if out, _ := strict.WrapToolCall(ctx, shellCall("git push origin feat"), next); out.Status != "error" {
		t.Fatal("no approver => deny")
	}
}

// --------------------------------------------------------------- the rest

func TestRepairOrphanedToolCalls(t *testing.T) {
	ai := messages.AI("", messages.ToolCall{ID: "a", Name: "execute"}, messages.ToolCall{ID: "b", Name: "read_file"})
	msgs := []messages.Message{messages.Human("go"), ai, messages.Tool("done", "a"), messages.Human("continue")}
	fixed, changed := guards.Repair(msgs)
	if !changed || len(fixed) != 5 {
		t.Fatalf("%d %v", len(fixed), changed)
	}
	var synth messages.Message
	for _, m := range fixed {
		if m.Role == messages.RoleTool && m.ToolCallID == "b" {
			synth = m
		}
	}
	if synth.Status != "error" || !strings.Contains(synth.Text(), "did not complete") || synth.Name != "read_file" {
		t.Fatalf("%+v", synth)
	}
	// the synthetic result sits before the next human message
	if fixed[3].ToolCallID != "b" && fixed[2].ToolCallID != "b" {
		t.Fatalf("misplaced: %+v", fixed)
	}
	if _, changed := guards.Repair(fixed); changed {
		t.Fatal("repair must be idempotent")
	}
	if _, changed := guards.Repair([]messages.Message{messages.Human("x"), messages.AI("no calls")}); changed {
		t.Fatal("nothing to repair")
	}
	// end to end: an interrupted thread no longer wedges the provider request
	var seen []messages.Message
	model := &models.Fake{Func: func(ms []messages.Message, _ models.Options) (messages.Message, error) {
		seen = ms
		answered := map[string]bool{}
		for _, m := range ms {
			if m.Role == messages.RoleTool {
				answered[m.ToolCallID] = true
			}
		}
		for _, m := range ms {
			for _, tc := range m.ToolCalls {
				if !answered[tc.ID] {
					return messages.Message{}, errors.New("provider rejected: tool_use without tool_result")
				}
			}
		}
		return messages.AI("recovered"), nil
	}}
	ag, err := agents.Create(agents.Options{Model: model, Middleware: []agents.Middleware{guards.RepairOrphanedToolCalls{}}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": []messages.Message{messages.Human("go"), ai, messages.Human("are you there?")}})
	if err != nil {
		t.Fatalf("wedged: %v", err)
	}
	last, _ := messages.Last(out["messages"].([]messages.Message))
	if last.Text() != "recovered" || len(seen) < 4 {
		t.Fatalf("%q", last.Text())
	}
}

func TestToolErrorHandlerAndCircuitBreaker(t *testing.T) {
	ctx := runnables.WithConfig(context.Background(), graph.Thread("t1"))
	boom := func(context.Context, messages.ToolCall) (messages.Message, error) {
		return messages.Message{}, errors.New("disk full")
	}
	h := guards.ToolErrorHandler{}
	out, err := h.WrapToolCall(ctx, messages.ToolCall{ID: "1", Name: "execute"}, boom)
	if err != nil || out.Status != "error" || !strings.Contains(out.Text(), "disk full") || out.ToolCallID != "1" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := h.WrapToolCall(ctx, messages.ToolCall{}, func(context.Context, messages.ToolCall) (messages.Message, error) {
		return messages.Message{}, context.Canceled
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must propagate: %v", err)
	}

	var notified []string
	br := &guards.SandboxCircuitBreaker{Threshold: 3, Notify: func(_ context.Context, thread string, err error) { notified = append(notified, thread) }}
	down := func(context.Context, messages.ToolCall) (messages.Message, error) {
		return messages.Message{}, errors.New("dial tcp: connection refused")
	}
	var ok prebuilt.ToolCaller = func(context.Context, messages.ToolCall) (messages.Message, error) {
		return messages.Tool("fine", "x"), nil
	}
	for i := 1; i <= 2; i++ {
		out, err := br.WrapToolCall(ctx, messages.ToolCall{ID: "x", Name: "execute"}, down)
		if err != nil || out.Status != "error" || !strings.Contains(out.Text(), "did not respond") {
			t.Fatalf("%d: below the threshold the model sees an error: %+v %v", i, out, err)
		}
	}
	// a success resets the count
	_, _ = br.WrapToolCall(ctx, messages.ToolCall{}, ok)
	for i := 1; i <= 2; i++ {
		if _, err := br.WrapToolCall(ctx, messages.ToolCall{ID: "x"}, down); err != nil {
			t.Fatalf("%v", err)
		}
	}
	_, err = br.WrapToolCall(ctx, messages.ToolCall{ID: "x"}, down)
	if !errors.Is(err, guards.ErrSandboxDown) || len(notified) != 1 || notified[0] != "t1" {
		t.Fatalf("%v %v", err, notified)
	}
	// fatal: ToolErrorHandler must let it end the run, and the user is told only once
	if _, err := h.WrapToolCall(ctx, messages.ToolCall{ID: "x"}, func(c context.Context, call messages.ToolCall) (messages.Message, error) {
		return br.WrapToolCall(c, call, down)
	}); !errors.Is(err, guards.ErrSandboxDown) || len(notified) != 1 {
		t.Fatalf("%v %v", err, notified)
	}
	// non-transient failures are not counted
	logic := func(context.Context, messages.ToolCall) (messages.Message, error) {
		return messages.Message{}, errors.New("exit status 1")
	}
	for range 5 {
		if _, err := br.WrapToolCall(ctx, messages.ToolCall{ID: "y"}, logic); errors.Is(err, guards.ErrSandboxDown) {
			t.Fatal("a failing command is not a dead sandbox")
		}
	}
	_ = sandbox.ErrUnreachable
}

func TestModelTimeout(t *testing.T) {
	slow := func(ctx context.Context, _ *agents.ModelRequest) (messages.Message, error) {
		<-ctx.Done()
		return messages.Message{}, ctx.Err()
	}
	_, err := guards.ModelTimeout{D: 30 * time.Millisecond}.WrapModelCall(context.Background(), &agents.ModelRequest{}, slow)
	if err == nil || !strings.Contains(err.Error(), "timed out after 30ms") {
		t.Fatalf("%v", err)
	}
	fast := func(context.Context, *agents.ModelRequest) (messages.Message, error) { return messages.AI("ok"), nil }
	if out, err := (guards.ModelTimeout{}).WrapModelCall(context.Background(), &agents.ModelRequest{}, fast); err != nil || out.Text() != "ok" {
		t.Fatal("zero = no timeout")
	}
}

func TestQueuedFollowUpsReachTheRunningAgent(t *testing.T) {
	q := &guards.MemoryQueue{}
	var calls [][]messages.Message
	weather := tools.New("noop", "noop", func(context.Context, struct{}) (any, error) {
		// a human sends a follow-up while this tool is running
		q.Push("thr", messages.Human("also update the changelog"))
		return "done", nil
	})
	model := &models.Fake{Func: func(ms []messages.Message, _ models.Options) (messages.Message, error) {
		calls = append(calls, ms)
		if len(calls) == 1 {
			return messages.AI("", messages.ToolCall{ID: "1", Name: "noop", Args: map[string]any{}}), nil
		}
		return messages.AI("ok, including the changelog"), nil
	}}
	ag, err := agents.Create(agents.Options{Model: model, Tools: []tools.Tool{weather}, Middleware: []agents.Middleware{guards.QueueMiddleware{Queue: q}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("thr"))
	if _, err := ag.Invoke(ctx, graph.State{"messages": "fix the bug"}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range calls[len(calls)-1] {
		found = found || m.Text() == "also update the changelog"
	}
	if !found || q.Len("thr") != 0 {
		t.Fatal("the follow-up must reach the next model call, and the queue must drain")
	}
	// other threads' queues are separate
	q.Push("other", messages.Human("x"))
	if q.Len("thr") != 0 || q.Len("other") != 1 {
		t.Fatal("isolation")
	}
}

func TestReadOnlyGuard(t *testing.T) {
	deny := []string{
		"git commit -am x", "git push origin feat", "git -C /w push", "cd r && git reset --hard HEAD~1", "git rebase main", "git stash",
		"gh pr review 5 --approve", "gh pr merge 5", "gh pr comment 5 -b hi", "gh pr ready 5", "gh issue close 3", "gh -R o/r pr edit 5 --title x",
		"gh api repos/o/r/pulls/5/reviews -f event=APPROVE", "gh api -X DELETE repos/o/r/git/refs/heads/x", "gh run rerun 9",
		`curl -X POST https://api.github.com/repos/o/r/issues/5/comments -d '{"body":"x"}'`,
		"bash -c 'git push'", "git push $(echo origin)",
	}
	for _, c := range deny {
		if guards.Mutation(c) == "" {
			t.Errorf("read-only must deny: %s", c)
		}
	}
	allow := []string{
		"git log --oneline -20", "git diff main...HEAD", "git show HEAD:README.md", "git status", "git checkout -b scratch", "git blame -L1,10 a.go",
		"gh pr view 5 --json files", "gh pr diff 5", "gh pr checks 5", "gh api repos/o/r/pulls/5/comments", "gh api -X GET repos/o/r/pulls",
		"gh run view 9 --log-failed", "curl -s https://api.github.com/repos/o/r/pulls/5", "grep -rn commit .", "go test ./...", "cat go.mod",
	}
	for _, c := range allow {
		if why := guards.Mutation(c); why != "" {
			t.Errorf("read-only must allow %q (flagged: %s)", c, why)
		}
	}
	ran := 0
	next := func(context.Context, messages.ToolCall) (messages.Message, error) {
		ran++
		return messages.Tool("ok", "c1"), nil
	}
	out, _ := guards.ReadOnlyGuard{}.WrapToolCall(context.Background(), shellCall("gh pr review 5 --approve"), next)
	if out.Status != "error" || ran != 0 || !strings.Contains(out.Text(), "publish_review") {
		t.Fatalf("%+v", out)
	}
	if _, err := (guards.ReadOnlyGuard{}).WrapToolCall(context.Background(), shellCall("git diff"), next); err != nil || ran != 1 {
		t.Fatal("reads pass")
	}
}
