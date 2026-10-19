package sandbox_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
)

func newManager(t *testing.T) (*sandbox.Manager, string) {
	base := t.TempDir()
	return &sandbox.Manager{Providers: map[string]sandbox.Provider{"local": sandbox.Local{Base: base}}, Default: "local", Store: store.NewMemory()}, base
}

func TestThreadsGetPersistentSeparateSandboxes(t *testing.T) {
	ctx := context.Background()
	m, base := newManager(t)
	a, err := m.ForThread(ctx, "thread/A 1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Dir(), base) || strings.Contains(filepath.Base(a.Dir()), "/") {
		t.Fatalf("dir %s", a.Dir())
	}
	again, _ := m.ForThread(ctx, "thread/A 1")
	b, _ := m.ForThread(ctx, "B")
	if again.ID() != a.ID() || b.ID() == a.ID() {
		t.Fatalf("%s %s %s", a.ID(), again.ID(), b.ID())
	}
	if _, err := a.Backend().Write(ctx, "/note.txt", "hello"); err != nil {
		t.Fatal(err)
	}
	// a new Manager over the same store finds the same workspace (restart)
	m2 := &sandbox.Manager{Providers: m.Providers, Default: "local", Store: m.Store}
	a2, err := m2.ForThread(ctx, "thread/A 1")
	if err != nil || a2.ID() != a.ID() {
		t.Fatalf("%v", err)
	}
	if got, err := a2.Backend().Read(ctx, "/note.txt"); err != nil || !strings.Contains(got, "hello") {
		t.Fatalf("workspace not persistent: %q %v", got, err)
	}
	if _, err := m.ForThread(ctx, ""); err == nil {
		t.Fatal("thread id required")
	}
}

func TestUnreachableSandboxIsNeverSilentlyReplaced(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager(t)
	sb, _ := m.ForThread(ctx, "t1")
	_, _ = sb.Backend().Write(ctx, "/work.txt", "uncommitted")
	if err := os.RemoveAll(sb.Dir()); err != nil {
		t.Fatal(err)
	}
	_, err := m.ForThread(ctx, "t1")
	if !errors.Is(err, sandbox.ErrUnreachable) || !sandbox.IsTransient(err) || !strings.Contains(err.Error(), "no replacement was started") {
		t.Fatalf("%v", err)
	}
	if _, statErr := os.Stat(sb.Dir()); statErr == nil {
		t.Fatal("the manager must not have recreated the directory behind the user's back")
	}
	// an explicit replace is the only way out
	fresh, err := m.Replace(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Backend().Read(ctx, "/work.txt"); err == nil {
		t.Fatal("a replacement sandbox is empty")
	}
	if sandbox.IsTransient(errors.New("exit status 1")) || sandbox.IsTransient(nil) || !sandbox.IsTransient(errors.New("dial tcp: connection refused")) {
		t.Fatal("IsTransient classification")
	}
}

func TestThreadBackendRoutesByThread(t *testing.T) {
	m, _ := newManager(t)
	be := m.Backend()
	ctxA := runnables.WithConfig(context.Background(), graph.Thread("A"))
	ctxB := runnables.WithConfig(context.Background(), graph.Thread("B"))
	if _, err := be.Write(ctxA, "/who.txt", "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := be.Read(ctxB, "/who.txt"); err == nil {
		t.Fatal("thread B must not see thread A's file")
	}
	if got, _ := be.Read(ctxA, "/who.txt"); !strings.Contains(got, "A") {
		t.Fatalf("%q", got)
	}
	r, err := be.Execute(ctxA, "cat who.txt", 10*time.Second)
	if err != nil || strings.TrimSpace(r.Output) != "A" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := be.Execute(context.Background(), "true", time.Second); err == nil {
		t.Fatal("no thread id in the context must be an error, not a shared sandbox")
	}
	var _ backends.Executor = be
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPrepareRepoClonesContinuesAndKeepsWork(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	// a "remote": a bare-ish upstream with one commit on main
	up := filepath.Join(t.TempDir(), "up")
	_ = os.MkdirAll(up, 0o755)
	gitRun(t, up, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(up, "README.md"), []byte("hi"), 0o644)
	gitRun(t, up, "add", ".")
	gitRun(t, up, "commit", "-q", "-m", "init")

	m, _ := newManager(t)
	sb, _ := m.ForThread(ctx, "thr")
	ex, _ := backends.AsExecutor(sb.Backend())
	id := sandbox.GitIdentity{Name: "Open SWE", Email: "bot@example.com"}
	spec := sandbox.RepoSpec{Owner: "o", Name: "proj", CloneURL: up, Branch: "openswe/thr"}

	st, err := sandbox.PrepareRepo(ctx, ex, spec, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Existing || st.Branch != "openswe/thr" || st.Base != "main" || len(st.HeadSHA) != 40 || len(st.Dirty) != 0 {
		t.Fatalf("%+v", st)
	}
	work := filepath.Join(sb.Dir(), "proj")
	if got := gitRun(t, work, "config", "user.email"); got != "bot@example.com" {
		t.Fatalf("identity %q", got)
	}
	// uncommitted work from a previous run survives the next PrepareRepo
	_ = os.WriteFile(filepath.Join(work, "wip.txt"), []byte("work in progress"), 0o644)
	st2, err := sandbox.PrepareRepo(ctx, ex, spec, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !st2.Existing || st2.Branch != "openswe/thr" || len(st2.Dirty) != 1 || !strings.Contains(st2.Dirty[0], "wip.txt") {
		t.Fatalf("a follow-up run must continue the work: %+v", st2)
	}
	if _, err := os.Stat(filepath.Join(work, "wip.txt")); err != nil {
		t.Fatal("work in progress was destroyed")
	}
	// the token never lands in the remote URL
	if _, err := sandbox.PrepareRepo(ctx, ex, spec, id, "ghs_secret"); err != nil {
		t.Fatal(err)
	}
	if remote := gitRun(t, work, "remote", "-v"); strings.Contains(remote, "ghs_secret") {
		t.Fatalf("token leaked into the remote URL: %s", remote)
	}
	// bad clone URL: a clear error
	_, err = sandbox.PrepareRepo(ctx, ex, sandbox.RepoSpec{Name: "x", CloneURL: "/definitely/not/a/repo", Dir: "x"}, id, "")
	if err == nil || !strings.Contains(err.Error(), "cloning") {
		t.Fatalf("%v", err)
	}
}
