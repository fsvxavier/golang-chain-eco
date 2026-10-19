// Package sandbox gives each agent thread its own persistent Linux workspace.
// A thread keeps its sandbox between runs (follow-ups reuse the checkout and
// the uncommitted work), and an unreachable sandbox is never silently replaced:
// a fresh one is empty, so swapping it in would throw away anything not yet
// pushed while still looking like a recovery.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/integrations/kubernetes"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// ErrUnreachable means the thread's sandbox stopped answering. It may come
// back; nothing replaces it automatically.
var ErrUnreachable = errors.New("sandbox unreachable")

// IsTransient reports whether err looks like the sandbox (not the command)
// failing: unreachable, a network timeout, or a refused connection.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnreachable) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection refused", "connection reset", "no such host", "broken pipe", "sandbox not found", "pod not found", "eof"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// Spec asks a provider for a sandbox.
type Spec struct {
	ThreadID string
	// Image is provider specific (a container image, a snapshot name).
	Image string
}

// Sandbox is one thread's workspace.
type Sandbox interface {
	// ID is persisted so the sandbox can be found again.
	ID() string
	// Dir is the working directory inside the sandbox.
	Dir() string
	// Backend gives file and shell access.
	Backend() backends.Backend
	// Ping fails when the sandbox cannot be reached.
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// Provider creates and reconnects sandboxes.
type Provider interface {
	Name() string
	Create(ctx context.Context, spec Spec) (Sandbox, error)
	Connect(ctx context.Context, id string) (Sandbox, error)
}

var slugRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Slug makes a string safe for paths and branch names.
func Slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(s, "-"), "-.")
	if s == "" {
		return "thread"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// ----------------------------------------------------------------- local

// Local runs sandboxes as directories on the host: no isolation. Use it for
// development, tests and trusted single-user setups; use Kubernetes or another
// remote provider for anything else.
type Local struct {
	// Base is the parent directory of the per-thread workspaces.
	Base string
	// Env replaces the child environment when non-nil.
	Env []string
}

// Name implements Provider.
func (l Local) Name() string { return "local" }

type localSandbox struct {
	dir string
	be  backends.LocalShell
}

func (s *localSandbox) ID() string                  { return "local:" + s.dir }
func (s *localSandbox) Dir() string                 { return s.dir }
func (s *localSandbox) Backend() backends.Backend   { return s.be }
func (s *localSandbox) Close(context.Context) error { return nil }
func (s *localSandbox) Ping(context.Context) error {
	if st, err := os.Stat(s.dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%w: workspace %s is gone", ErrUnreachable, s.dir)
	}
	return nil
}

// Create implements Provider.
func (l Local) Create(_ context.Context, spec Spec) (Sandbox, error) {
	if l.Base == "" {
		return nil, errors.New("sandbox: Local.Base is required")
	}
	dir, err := filepath.Abs(filepath.Join(l.Base, Slug(spec.ThreadID)))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return l.open(dir), nil
}

func (l Local) open(dir string) Sandbox {
	return &localSandbox{dir: dir, be: backends.LocalShell{Filesystem: backends.Filesystem{Root: dir}, Env: l.Env}}
}

// Connect implements Provider.
func (l Local) Connect(_ context.Context, id string) (Sandbox, error) {
	dir := strings.TrimPrefix(id, "local:")
	sb := l.open(dir)
	return sb, sb.Ping(context.Background())
}

// ------------------------------------------------------------ kubernetes

// Kubernetes runs each sandbox as a pod (integrations/kubernetes).
type Kubernetes struct {
	Client    *kubernetes.Client
	Namespace string
	// Image is the default pod image (needs sh, GNU find/grep, base64, git).
	Image string
}

// Name implements Provider.
func (k Kubernetes) Name() string { return "kubernetes" }

type k8sSandbox struct {
	sb *kubernetes.Sandbox
	be backends.Remote
}

func (s *k8sSandbox) ID() string                      { return "k8s:" + s.sb.Namespace + "/" + s.sb.Pod }
func (s *k8sSandbox) Dir() string                     { return "/workspace" }
func (s *k8sSandbox) Backend() backends.Backend       { return s.be }
func (s *k8sSandbox) Close(ctx context.Context) error { return s.sb.Close(ctx) }
func (s *k8sSandbox) Ping(ctx context.Context) error {
	r, err := s.sb.Run(ctx, "true", 15*time.Second)
	if err != nil || r.ExitCode != 0 {
		return fmt.Errorf("%w: pod %s: %v", ErrUnreachable, s.sb.Pod, err)
	}
	return nil
}

func wrapK8s(sb *kubernetes.Sandbox) Sandbox {
	return &k8sSandbox{sb: sb, be: backends.Remote{Run: sb.Run, Root: "/workspace"}}
}

// Create implements Provider.
func (k Kubernetes) Create(ctx context.Context, spec Spec) (Sandbox, error) {
	image := spec.Image
	if image == "" {
		image = k.Image
	}
	if image == "" {
		return nil, errors.New("sandbox: no image configured for the Kubernetes provider")
	}
	sb, err := kubernetes.NewSandbox(ctx, k.Client, k.Namespace, image)
	if err != nil {
		return nil, err
	}
	return wrapK8s(sb), nil
}

// Connect implements Provider.
func (k Kubernetes) Connect(ctx context.Context, id string) (Sandbox, error) {
	rest := strings.TrimPrefix(id, "k8s:")
	ns, pod, ok := strings.Cut(rest, "/")
	if !ok {
		return nil, fmt.Errorf("sandbox: bad kubernetes sandbox id %q", id)
	}
	p, err := k.Client.GetPod(ctx, ns, pod)
	if err != nil || p.Phase != "Running" {
		return nil, fmt.Errorf("%w: pod %s/%s: %v", ErrUnreachable, ns, pod, err)
	}
	return wrapK8s(&kubernetes.Sandbox{C: k.Client, Namespace: ns, Pod: pod}), nil
}

// ---------------------------------------------------------------- manager

// Manager hands each thread its sandbox and remembers which one it is.
type Manager struct {
	Providers map[string]Provider
	// Default names the provider used for new sandboxes.
	Default string
	// Store persists thread -> sandbox (default: in memory, lost on restart).
	Store store.Store
	// Image overrides the provider's default image.
	Image string

	mu   sync.Mutex
	live map[string]Sandbox
}

func nsSandboxes() []string { return []string{"openswe", "sandboxes"} }

func (m *Manager) store() store.Store {
	if m.Store == nil {
		m.Store = store.NewMemory()
	}
	return m.Store
}

func (m *Manager) provider(name string) (Provider, error) {
	p := m.Providers[name]
	if p == nil {
		return nil, fmt.Errorf("sandbox: unknown provider %q", name)
	}
	return p, nil
}

// ForThread returns the thread's sandbox, creating it on first use. A stored
// sandbox that cannot be reached yields ErrUnreachable; it is NOT replaced
// (use Replace, which is a deliberate, user-visible act).
func (m *Manager) ForThread(ctx context.Context, threadID string) (Sandbox, error) {
	if threadID == "" {
		return nil, errors.New("sandbox: a thread id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sb, ok := m.live[threadID]; ok {
		if err := sb.Ping(ctx); err != nil {
			return nil, m.unreachable(threadID, sb.ID(), err)
		}
		return sb, nil
	}
	it, err := m.store().Get(ctx, nsSandboxes(), threadID)
	if err != nil {
		return nil, err
	}
	if it != nil {
		provName, _ := it.Value["provider"].(string)
		id, _ := it.Value["id"].(string)
		p, err := m.provider(provName)
		if err != nil {
			return nil, err
		}
		sb, err := p.Connect(ctx, id)
		if err == nil {
			err = sb.Ping(ctx)
		}
		if err != nil {
			return nil, m.unreachable(threadID, id, err)
		}
		m.remember(threadID, sb)
		return sb, nil
	}
	p, err := m.provider(m.Default)
	if err != nil {
		return nil, err
	}
	sb, err := p.Create(ctx, Spec{ThreadID: threadID, Image: m.Image})
	if err != nil {
		return nil, fmt.Errorf("sandbox: creating the sandbox for thread %s: %w", threadID, err)
	}
	if err := m.store().Put(ctx, nsSandboxes(), threadID, map[string]any{"provider": p.Name(), "id": sb.ID()}); err != nil {
		_ = sb.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	m.remember(threadID, sb)
	return sb, nil
}

func (m *Manager) remember(thread string, sb Sandbox) {
	if m.live == nil {
		m.live = map[string]Sandbox{}
	}
	m.live[thread] = sb
}

func (m *Manager) unreachable(thread, id string, cause error) error {
	delete(m.live, thread)
	return fmt.Errorf("%w: the sandbox of thread %s (%s) stopped responding and no replacement was started: a new sandbox is empty, so swapping one in would discard work that is not committed and pushed. Retrigger the thread to try the same sandbox again, or replace it explicitly: %v",
		ErrUnreachable, thread, id, cause)
}

// Replace discards the thread's sandbox record and creates a new, empty
// sandbox. Call it only when the user asked for it.
func (m *Manager) Replace(ctx context.Context, threadID string) (Sandbox, error) {
	m.mu.Lock()
	if old, ok := m.live[threadID]; ok {
		_ = old.Close(context.WithoutCancel(ctx))
		delete(m.live, threadID)
	}
	m.mu.Unlock()
	if err := m.store().Delete(ctx, nsSandboxes(), threadID); err != nil {
		return nil, err
	}
	return m.ForThread(ctx, threadID)
}

// Backend returns a backends.Backend (and Executor) that resolves the
// calling thread's sandbox from the run context at each call, so one agent
// graph serves many threads.
func (m *Manager) Backend() *ThreadBackend { return &ThreadBackend{M: m} }

// ThreadBackend resolves the sandbox per call from Configurable["thread_id"].
type ThreadBackend struct{ M *Manager }

var (
	_ backends.Backend  = (*ThreadBackend)(nil)
	_ backends.Executor = (*ThreadBackend)(nil)
)

func (t *ThreadBackend) be(ctx context.Context) (backends.Backend, error) {
	thread, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	sb, err := t.M.ForThread(ctx, thread)
	if err != nil {
		return nil, err
	}
	return sb.Backend(), nil
}

func (t *ThreadBackend) Ls(ctx context.Context, dir string) ([]backends.FileInfo, error) {
	b, err := t.be(ctx)
	if err != nil {
		return nil, err
	}
	return b.Ls(ctx, dir)
}

func (t *ThreadBackend) Read(ctx context.Context, p string) (string, error) {
	b, err := t.be(ctx)
	if err != nil {
		return "", err
	}
	return b.Read(ctx, p)
}

func (t *ThreadBackend) Write(ctx context.Context, p, content string) (backends.FilesUpdate, error) {
	b, err := t.be(ctx)
	if err != nil {
		return nil, err
	}
	return b.Write(ctx, p, content)
}

func (t *ThreadBackend) Edit(ctx context.Context, p, old, new string, all bool) (backends.FilesUpdate, int, error) {
	b, err := t.be(ctx)
	if err != nil {
		return nil, 0, err
	}
	return b.Edit(ctx, p, old, new, all)
}

func (t *ThreadBackend) Glob(ctx context.Context, pattern, dir string) ([]backends.FileInfo, error) {
	b, err := t.be(ctx)
	if err != nil {
		return nil, err
	}
	return b.Glob(ctx, pattern, dir)
}

func (t *ThreadBackend) Grep(ctx context.Context, pattern, dir, glob string) ([]backends.GrepMatch, error) {
	b, err := t.be(ctx)
	if err != nil {
		return nil, err
	}
	return b.Grep(ctx, pattern, dir, glob)
}

// Execute implements backends.Executor.
func (t *ThreadBackend) Execute(ctx context.Context, command string, timeout time.Duration) (backends.ExecResult, error) {
	b, err := t.be(ctx)
	if err != nil {
		return backends.ExecResult{}, err
	}
	ex, ok := backends.AsExecutor(b)
	if !ok {
		return backends.ExecResult{}, errors.New("sandbox: this provider cannot execute commands")
	}
	return ex.Execute(ctx, command, timeout)
}
