package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

// Sandbox is a throwaway pod used as an isolated workspace for an agent.
// The image needs a POSIX shell, GNU find/grep, base64 and sleep (any
// Debian/Ubuntu image).
type Sandbox struct {
	C         *Client
	Namespace string
	Pod       string
	// MaxOutput bounds captured command output (default 100 KiB).
	MaxOutput int
}

// NewSandbox starts a pod from image and waits until it is running.
func NewSandbox(ctx context.Context, c *Client, namespace, image string) (*Sandbox, error) {
	name := "lcg-sandbox-" + messages.NewID()[:8]
	if err := c.CreatePod(ctx, namespace, name, image, []string{"sleep", "infinity"}, map[string]string{"app": "lcg-sandbox"}); err != nil {
		return nil, err
	}
	sb := &Sandbox{C: c, Namespace: namespace, Pod: name}
	for {
		p, err := c.GetPod(ctx, namespace, name)
		if err != nil {
			sb.Close(context.WithoutCancel(ctx))
			return nil, err
		}
		if p.Phase == "Running" {
			if r, err := sb.Run(ctx, "mkdir -p /workspace", 30*time.Second); err != nil || r.ExitCode != 0 {
				sb.Close(context.WithoutCancel(ctx))
				return nil, fmt.Errorf("kubernetes: preparing /workspace: exit %d: %v", r.ExitCode, err)
			}
			return sb, nil
		}
		if p.Phase == "Failed" || p.Phase == "Succeeded" {
			sb.Close(context.WithoutCancel(ctx))
			return nil, fmt.Errorf("kubernetes: sandbox pod ended in phase %s", p.Phase)
		}
		select {
		case <-ctx.Done():
			sb.Close(context.WithoutCancel(ctx))
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Close deletes the pod.
func (s *Sandbox) Close(ctx context.Context) error { return s.C.DeletePod(ctx, s.Namespace, s.Pod) }

// Run executes a shell command in the pod (backends.Remote's primitive).
func (s *Sandbox) Run(ctx context.Context, command string, timeout time.Duration) (backends.ExecResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	max := s.MaxOutput
	if max <= 0 {
		max = 100 << 10
	}
	r, err := s.C.Exec(ctx, s.Namespace, s.Pod, "", []string{"sh", "-c", command}, max)
	res := backends.ExecResult{Output: r.Output, ExitCode: r.ExitCode}
	if len(res.Output) > max {
		res.Output, res.Truncated = res.Output[:max], true
	}
	if ctx.Err() != nil {
		return backends.ExecResult{Output: res.Output + "\n[command timed out]", ExitCode: -1}, nil
	}
	return res, err
}

// Backend returns a deepagents backend (files + shell) rooted at "/workspace".
func (s *Sandbox) Backend() backends.Remote {
	return backends.Remote{Run: s.Run, Root: "/workspace"}
}
