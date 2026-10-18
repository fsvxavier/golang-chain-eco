package backends

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Filesystem stores files on disk under Root. Virtual paths ("/a/b") map to
// Root/a/b; anything escaping Root is rejected.
type Filesystem struct{ Root string }

func (f Filesystem) real(p string) (string, error) {
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(Clean(p)))
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the backend root", p)
	}
	// resolve symlinks of the deepest existing ancestor
	probe := full
	for {
		if r, err := filepath.EvalSymlinks(probe); err == nil {
			rr, _ := filepath.EvalSymlinks(root)
			if r != rr && !strings.HasPrefix(r, rr+string(filepath.Separator)) {
				return "", fmt.Errorf("path %q escapes the backend root via symlink", p)
			}
			break
		}
		next := filepath.Dir(probe)
		if next == probe {
			break
		}
		probe = next
	}
	return full, nil
}

func (f Filesystem) virtual(full string) string {
	root, _ := filepath.Abs(f.Root)
	rel, _ := filepath.Rel(root, full)
	return Clean(filepath.ToSlash(rel))
}

func (f Filesystem) Ls(_ context.Context, dir string) ([]FileInfo, error) {
	full, err := f.real(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	var out []FileInfo
	for _, e := range ents {
		fi := FileInfo{Path: path.Join(Clean(dir), e.Name()), IsDir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			fi.Size, fi.ModifiedAt = int(info.Size()), info.ModTime().Format(time.RFC3339)
		}
		if fi.IsDir {
			fi.Path += "/"
		}
		out = append(out, fi)
	}
	return out, nil
}

func (f Filesystem) Read(_ context.Context, p string) (string, error) {
	full, err := f.real(p)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	return string(b), err
}

func (f Filesystem) Write(_ context.Context, p, content string) (FilesUpdate, error) {
	full, err := f.real(p)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("cannot write to %s because it already exists; read it and make an edit, or write to a new path", p)
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return nil, err
}

func (f Filesystem) Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error) {
	c, err := f.Read(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	c, n, err := ApplyEdit(c, old, new, all)
	if err != nil {
		return nil, 0, err
	}
	full, _ := f.real(p)
	return nil, n, os.WriteFile(full, []byte(c), 0o644)
}

// walk loads every regular file under dir (text only, skips files > 5 MB).
func (f Filesystem) walk(dir string) (map[string]FileData, error) {
	full, err := f.real(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]FileData{}
	err = filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 5<<20 || !info.Mode().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		out[f.virtual(p)] = FileData{Content: string(b), ModifiedAt: info.ModTime()}
		return nil
	})
	return out, err
}

func (f Filesystem) Glob(_ context.Context, pattern, dir string) ([]FileInfo, error) {
	files, err := f.walk(dir)
	if err != nil {
		return nil, err
	}
	return globFiles(files, pattern, dir)
}

func (f Filesystem) Grep(_ context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	files, err := f.walk(dir)
	if err != nil {
		return nil, err
	}
	return grepFiles(files, pattern, dir, glob)
}

// LocalShell adds command execution to Filesystem. It runs commands directly
// on the host (no sandbox): only enable it for trusted agents/inputs.
type LocalShell struct {
	Filesystem
	// Env, if non-nil, replaces the child environment.
	Env []string
	// MaxOutput bounds captured bytes (default 100 KiB).
	MaxOutput int
}

func (s LocalShell) Execute(ctx context.Context, command string, timeout time.Duration) (ExecResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	root, _ := filepath.Abs(s.Root)
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir, cmd.Env = root, s.Env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	res := ExecResult{Output: buf.String()}
	if max := orDefault(s.MaxOutput, 100<<10); len(res.Output) > max {
		res.Output, res.Truncated = res.Output[:max], true
	}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
		return res, nil
	case ctx.Err() != nil:
		res.ExitCode = -1
		res.Output += "\n[command timed out]"
		return res, nil
	}
	return res, err
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// Composite routes paths to different backends by prefix, e.g. "/memories/"
// to a Store backend and everything else to State.
type Composite struct {
	Default Backend
	// Routes maps a prefix such as "/memories/" to its backend.
	Routes map[string]Backend
}

func (c Composite) route(p string) (Backend, string, string) {
	p = Clean(p)
	best := ""
	for pre := range c.Routes {
		pp := strings.TrimSuffix(pre, "/")
		if (p == pp || strings.HasPrefix(p, pp+"/")) && len(pre) > len(best) {
			best = pre
		}
	}
	if best == "" {
		return c.Default, p, ""
	}
	pp := strings.TrimSuffix(best, "/")
	rest := strings.TrimPrefix(p, pp)
	if rest == "" {
		rest = "/"
	}
	return c.Routes[best], rest, pp
}

func prefixInfos(infos []FileInfo, pre string) []FileInfo {
	for i := range infos {
		infos[i].Path = pre + infos[i].Path
	}
	return infos
}

func (c Composite) Ls(ctx context.Context, dir string) ([]FileInfo, error) {
	b, rest, pre := c.route(dir)
	infos, err := b.Ls(ctx, rest)
	if err != nil {
		return nil, err
	}
	infos = prefixInfos(infos, pre)
	if pre == "" { // add route mount points that live directly under dir
		d := strings.TrimSuffix(Clean(dir), "/")
		for r := range c.Routes {
			rp := strings.TrimSuffix(r, "/")
			if path.Dir(rp) == path.Clean(d+"/.") || (d == "" && path.Dir(rp) == "/") {
				infos = append(infos, FileInfo{Path: rp + "/", IsDir: true})
			}
		}
		sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	}
	return infos, nil
}

func (c Composite) Read(ctx context.Context, p string) (string, error) {
	b, rest, _ := c.route(p)
	return b.Read(ctx, rest)
}

func (c Composite) Write(ctx context.Context, p, content string) (FilesUpdate, error) {
	b, rest, pre := c.route(p)
	u, err := b.Write(ctx, rest, content)
	return reprefix(u, pre), err
}

func (c Composite) Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error) {
	b, rest, pre := c.route(p)
	u, n, err := b.Edit(ctx, rest, old, new, all)
	return reprefix(u, pre), n, err
}

func reprefix(u FilesUpdate, pre string) FilesUpdate {
	if u == nil || pre == "" {
		return u
	}
	out := FilesUpdate{}
	for k, v := range u {
		out[pre+k] = v
	}
	return out
}

func (c Composite) all(dir string) []struct {
	b         Backend
	rest, pre string
} {
	type t = struct {
		b         Backend
		rest, pre string
	}
	b, rest, pre := c.route(dir)
	out := []t{{b, rest, pre}}
	if pre == "" { // also search routes below dir
		d := strings.TrimSuffix(Clean(dir), "/")
		for r, rb := range c.Routes {
			rp := strings.TrimSuffix(r, "/")
			if strings.HasPrefix(rp, d+"/") || d == "" {
				out = append(out, t{rb, "/", rp})
			}
		}
	}
	return out
}

func (c Composite) Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error) {
	var out []FileInfo
	for _, t := range c.all(dir) {
		r, err := t.b.Glob(ctx, pattern, t.rest)
		if err != nil {
			return nil, err
		}
		out = append(out, prefixInfos(r, t.pre)...)
	}
	return out, nil
}

func (c Composite) Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	var out []GrepMatch
	for _, t := range c.all(dir) {
		r, err := t.b.Grep(ctx, pattern, t.rest, glob)
		if err != nil {
			return nil, err
		}
		for i := range r {
			r[i].Path = t.pre + r[i].Path
		}
		out = append(out, r...)
	}
	return out, nil
}

// Execute delegates to the default backend when it can execute commands.
func (c Composite) Execute(ctx context.Context, command string, timeout time.Duration) (ExecResult, error) {
	if e, ok := c.Default.(Executor); ok {
		return e.Execute(ctx, command, timeout)
	}
	return ExecResult{}, errors.New("default backend does not support execute")
}
