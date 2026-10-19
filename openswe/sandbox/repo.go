package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

// RepoSpec describes a repository to work in.
type RepoSpec struct {
	Owner, Name string
	// CloneURL defaults to https://github.com/<owner>/<name>.git.
	CloneURL string
	// Base is the branch new work starts from (default: the remote default).
	Base string
	// Branch is the work branch to create or continue (e.g. openswe/<thread>).
	Branch string
	// Dir is the checkout directory name (default: the repository name).
	Dir string
}

// GitIdentity is who commits are authored by.
type GitIdentity struct {
	Name, Email string
}

// RepoState is what Prepare found or made.
type RepoState struct {
	Dir     string
	Branch  string
	Base    string
	HeadSHA string
	// Existing is true when the checkout was already there (a follow-up run).
	Existing bool
	// Dirty lists uncommitted files left by earlier runs.
	Dirty []string
}

func sh(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func run(ctx context.Context, ex backends.Executor, cmd string, timeout time.Duration) (string, error) {
	r, err := ex.Execute(ctx, cmd, timeout)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		return r.Output, fmt.Errorf("sandbox: command failed (exit %d): %s", r.ExitCode, firstLine(r.Output))
	}
	return strings.TrimSpace(r.Output), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// PrepareRepo makes the checkout ready: clone it if missing (otherwise fetch
// and keep the work in progress), set the commit identity, and be on the work
// branch. An existing checkout is never reset or cleaned, so a follow-up run
// continues the previous work. token, when non-empty, is stored in a
// credential file readable only by the user, never in the remote URL, so it
// does not show up in `git remote -v` or in tool output.
func PrepareRepo(ctx context.Context, ex backends.Executor, spec RepoSpec, id GitIdentity, token string) (RepoState, error) {
	dir := spec.Dir
	if dir == "" {
		dir = spec.Name
	}
	url := spec.CloneURL
	if url == "" {
		url = fmt.Sprintf("https://github.com/%s/%s.git", spec.Owner, spec.Name)
	}
	const t = 10 * time.Minute
	cred := ""
	if token != "" {
		cred = " -c credential.helper='store --file=\"$HOME/.openswe-git-credentials\"'"
		host := "github.com"
		if i := strings.Index(url, "://"); i >= 0 {
			host = strings.SplitN(url[i+3:], "/", 2)[0]
		}
		if _, err := run(ctx, ex, fmt.Sprintf(`umask 077 && printf 'https://x-access-token:%%s@%%s\n' %s %s > "$HOME/.openswe-git-credentials"`, sh(token), sh(host)), time.Minute); err != nil {
			return RepoState{}, err
		}
	}
	git := "git" + cred
	st := RepoState{Dir: dir}
	if _, err := run(ctx, ex, "test -d "+sh(dir+"/.git"), time.Minute); err == nil {
		st.Existing = true
		if _, err := run(ctx, ex, fmt.Sprintf("cd %s && %s fetch --prune origin", sh(dir), git), t); err != nil {
			return st, fmt.Errorf("fetching %s: %w", dir, err)
		}
	} else if _, err := run(ctx, ex, fmt.Sprintf("%s clone %s %s", git, sh(url), sh(dir)), t); err != nil {
		return st, fmt.Errorf("cloning %s: %w", url, err)
	}
	if _, err := run(ctx, ex, fmt.Sprintf("cd %s && git config user.name %s && git config user.email %s", sh(dir), sh(id.Name), sh(id.Email)), time.Minute); err != nil {
		return st, err
	}
	base := spec.Base
	if base == "" {
		out, err := run(ctx, ex, fmt.Sprintf("cd %s && git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null || git rev-parse --abbrev-ref origin/HEAD 2>/dev/null || echo origin/main", sh(dir)), time.Minute)
		if err != nil {
			return st, err
		}
		base = strings.TrimPrefix(strings.TrimSpace(out), "origin/")
	}
	st.Base = base
	if spec.Branch != "" {
		// continue the branch if it exists locally or on the remote, otherwise start it from base
		cmd := fmt.Sprintf(`cd %[1]s && if git rev-parse --verify --quiet %[2]s >/dev/null; then git checkout %[2]s; `+
			`elif git rev-parse --verify --quiet origin/%[2]s >/dev/null; then git checkout -b %[2]s origin/%[2]s; `+
			`else git checkout -b %[2]s origin/%[3]s 2>/dev/null || git checkout -b %[2]s; fi`, sh(dir), sh(spec.Branch), sh(base))
		if _, err := run(ctx, ex, cmd, time.Minute); err != nil {
			return st, fmt.Errorf("checking out %s: %w", spec.Branch, err)
		}
	}
	st.Branch, _ = run(ctx, ex, fmt.Sprintf("cd %s && git rev-parse --abbrev-ref HEAD", sh(dir)), time.Minute)
	st.HeadSHA, _ = run(ctx, ex, fmt.Sprintf("cd %s && git rev-parse HEAD", sh(dir)), time.Minute)
	if out, err := run(ctx, ex, fmt.Sprintf("cd %s && git status --porcelain", sh(dir)), time.Minute); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			st.Dirty = append(st.Dirty, strings.TrimSpace(l))
		}
	}
	return st, nil
}
