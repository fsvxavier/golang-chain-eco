package backends

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// Operations a permission rule can govern.
const (
	OpRead  = "read"  // ls, read_file, glob, grep
	OpWrite = "write" // write_file, edit_file
)

// Permission allows or denies operations on paths. Rules are evaluated in
// order; the first whose operations and path glob match decides. Paths with
// no matching rule are allowed.
type Permission struct {
	Operations []string // OpRead, OpWrite (empty = both)
	Paths      []string // globs such as "/secrets/**" (see GlobRegexp)
	Deny       bool
}

func (p Permission) matches(op, path string) bool {
	if len(p.Operations) > 0 && !slices.Contains(p.Operations, op) {
		return false
	}
	for _, g := range p.Paths {
		if re, err := GlobRegexp(g); err == nil && re.MatchString(Clean(path)) {
			return true
		}
	}
	return false
}

// Permissioned enforces Rules on every Backend call, so the policy also
// applies to subagents sharing the backend. Execute is blocked when any deny
// rule exists, because a shell can bypass path rules.
type Permissioned struct {
	Backend Backend
	Rules   []Permission
}

// WithPermissions wraps b with rules.
func WithPermissions(b Backend, rules ...Permission) Permissioned {
	return Permissioned{Backend: b, Rules: rules}
}

func (p Permissioned) allowed(op, path string) bool {
	for _, r := range p.Rules {
		if r.matches(op, path) {
			return !r.Deny
		}
	}
	return true
}

func (p Permissioned) check(op, path string) error {
	if !p.allowed(op, path) {
		return fmt.Errorf("permission denied: %s access to %s", op, Clean(path))
	}
	return nil
}

func (p Permissioned) Ls(ctx context.Context, dir string) ([]FileInfo, error) {
	if err := p.check(OpRead, dir); err != nil {
		return nil, err
	}
	infos, err := p.Backend.Ls(ctx, dir)
	return p.filterInfos(infos), err
}

func (p Permissioned) Read(ctx context.Context, path string) (string, error) {
	if err := p.check(OpRead, path); err != nil {
		return "", err
	}
	return p.Backend.Read(ctx, path)
}

func (p Permissioned) Write(ctx context.Context, path, content string) (FilesUpdate, error) {
	if err := p.check(OpWrite, path); err != nil {
		return nil, err
	}
	return p.Backend.Write(ctx, path, content)
}

func (p Permissioned) Edit(ctx context.Context, path, old, new string, all bool) (FilesUpdate, int, error) {
	if err := p.check(OpWrite, path); err != nil {
		return nil, 0, err
	}
	if err := p.check(OpRead, path); err != nil {
		return nil, 0, err
	}
	return p.Backend.Edit(ctx, path, old, new, all)
}

func (p Permissioned) Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error) {
	if err := p.check(OpRead, dir); err != nil {
		return nil, err
	}
	infos, err := p.Backend.Glob(ctx, pattern, dir)
	return p.filterInfos(infos), err
}

func (p Permissioned) Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	if err := p.check(OpRead, dir); err != nil {
		return nil, err
	}
	ms, err := p.Backend.Grep(ctx, pattern, dir, glob)
	out := ms[:0]
	for _, m := range ms {
		if p.allowed(OpRead, m.Path) {
			out = append(out, m)
		}
	}
	return out, err
}

func (p Permissioned) filterInfos(in []FileInfo) []FileInfo {
	out := in[:0]
	for _, fi := range in {
		if p.allowed(OpRead, fi.Path) {
			out = append(out, fi)
		}
	}
	return out
}

// Execute delegates to the wrapped backend unless deny rules exist.
func (p Permissioned) Execute(ctx context.Context, cmd string, timeout time.Duration) (ExecResult, error) {
	for _, r := range p.Rules {
		if r.Deny {
			return ExecResult{}, fmt.Errorf("permission denied: execute is disabled while deny rules are active")
		}
	}
	e, ok := AsExecutor(p.Backend)
	if !ok {
		return ExecResult{}, fmt.Errorf("backend does not support execute")
	}
	return e.Execute(ctx, cmd, timeout)
}
