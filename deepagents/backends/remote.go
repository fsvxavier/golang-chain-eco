package backends

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Remote implements Backend and Executor on top of a single primitive: run a
// shell command somewhere (a Kubernetes pod, a Docker container, an SSH host,
// a cloud sandbox). Every file operation becomes a POSIX shell snippet, so
// any sandbox that can execute a command becomes a full deepagents backend.
//
// The target needs a POSIX shell with GNU find and grep, base64 and mkdir
// (any Debian/Ubuntu/Alpine+coreutils image). Files travel base64-encoded in
// the command line, so single writes are limited to a few hundred KB.
type Remote struct {
	// Run executes command and returns combined output and exit code.
	Run func(ctx context.Context, command string, timeout time.Duration) (ExecResult, error)
	// Root is the directory the virtual "/" maps to (default "/").
	Root string
}

var (
	_ Backend  = Remote{}
	_ Executor = Remote{}
)

// maxRemoteWrite bounds one Write/Edit (the content rides in the command).
const maxRemoteWrite = 512 << 10

func (r Remote) Execute(ctx context.Context, command string, timeout time.Duration) (ExecResult, error) {
	if r.Root != "" && r.Root != "/" {
		command = "cd " + shq(r.Root) + " && " + command
	}
	return r.Run(ctx, command, timeout)
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// real maps a virtual path to the path on the target.
func (r Remote) real(p string) string {
	p = Clean(p)
	if r.Root == "" || r.Root == "/" {
		return p
	}
	return path.Join(r.Root, p)
}

func (r Remote) virtual(real string) string {
	if r.Root == "" || r.Root == "/" {
		return real
	}
	return Clean(strings.TrimPrefix(real, strings.TrimSuffix(r.Root, "/")))
}

func (r Remote) sh(ctx context.Context, cmd string) (ExecResult, error) {
	return r.Run(ctx, cmd, 60*time.Second)
}

func (r Remote) Read(ctx context.Context, p string) (string, error) {
	res, err := r.sh(ctx, fmt.Sprintf("[ -f %[1]s ] || exit 44; base64 < %[1]s", shq(r.real(p))))
	if err != nil {
		return "", err
	}
	switch res.ExitCode {
	case 0:
	case 44:
		return "", ErrNotFound
	default:
		return "", fmt.Errorf("read %s: exit %d: %s", p, res.ExitCode, strings.TrimSpace(res.Output))
	}
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(res.Output), ""))
	return string(b), err
}

func (r Remote) put(ctx context.Context, p, content string, mustBeNew bool) error {
	if len(content) > maxRemoteWrite {
		return fmt.Errorf("remote backend: %s is %d bytes; limit is %d", p, len(content), maxRemoteWrite)
	}
	full := shq(r.real(p))
	guard := ""
	if mustBeNew {
		guard = fmt.Sprintf("[ -e %s ] && exit 45; ", full)
	}
	res, err := r.sh(ctx, fmt.Sprintf("%smkdir -p \"$(dirname %s)\" && printf %%s %s | base64 -d > %s",
		guard, full, shq(base64.StdEncoding.EncodeToString([]byte(content))), full))
	if err != nil {
		return err
	}
	switch res.ExitCode {
	case 0:
		return nil
	case 45:
		return fmt.Errorf("cannot write to %s because it already exists; read it and make an edit, or write to a new path", p)
	}
	return fmt.Errorf("write %s: exit %d: %s", p, res.ExitCode, strings.TrimSpace(res.Output))
}

func (r Remote) Write(ctx context.Context, p, content string) (FilesUpdate, error) {
	return nil, r.put(ctx, p, content, true)
}

func (r Remote) Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error) {
	c, err := r.Read(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	c, n, err := ApplyEdit(c, old, new, all)
	if err != nil {
		return nil, 0, err
	}
	return nil, n, r.put(ctx, p, c, false)
}

// findFiles lists regular files (and optionally directories) under dir.
func (r Remote) find(ctx context.Context, dir string, depth1 bool) ([]FileInfo, error) {
	opts := "-type f"
	if depth1 {
		opts = "-mindepth 1 -maxdepth 1"
	}
	res, err := r.sh(ctx, fmt.Sprintf("[ -d %[1]s ] || exit 44; find %[1]s %[2]s -printf '%%y\\t%%s\\t%%T@\\t%%p\\n'", shq(r.real(dir)), opts))
	if err != nil {
		return nil, err
	}
	if res.ExitCode == 44 {
		return nil, ErrNotFound
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("find %s: exit %d: %s", dir, res.ExitCode, strings.TrimSpace(res.Output))
	}
	var out []FileInfo
	for _, l := range strings.Split(strings.TrimSpace(res.Output), "\n") {
		f := strings.SplitN(l, "\t", 4)
		if len(f) != 4 {
			continue
		}
		size, _ := strconv.Atoi(f[1])
		secs, _ := strconv.ParseFloat(f[2], 64)
		fi := FileInfo{Path: r.virtual(f[3]), IsDir: f[0] == "d", Size: size, ModifiedAt: time.Unix(int64(secs), 0).UTC().Format(time.RFC3339)}
		if fi.IsDir {
			fi.Path += "/"
			fi.Size = 0
		}
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (r Remote) Ls(ctx context.Context, dir string) ([]FileInfo, error) {
	return r.find(ctx, dir, true)
}

func (r Remote) Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error) {
	re, err := GlobRegexp(pattern)
	if err != nil {
		return nil, err
	}
	files, err := r.find(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	dir = Clean(dir)
	var out []FileInfo
	for _, f := range files {
		if matchGlob(re, pattern, dir, f.Path) {
			out = append(out, f)
		}
	}
	return out, nil
}

func (r Remote) Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error) {
	var re interface{ MatchString(string) bool }
	if glob != "" {
		g, err := GlobRegexp(glob)
		if err != nil {
			return nil, err
		}
		re = g
	}
	res, err := r.sh(ctx, fmt.Sprintf("grep -rnIF --null -e %s -- %s", shq(pattern), shq(r.real(dir))))
	if err != nil {
		return nil, err
	}
	if res.ExitCode > 1 { // 1 = no match
		return nil, fmt.Errorf("grep: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Output))
	}
	dir = Clean(dir)
	var out []GrepMatch
	for _, l := range strings.Split(res.Output, "\n") {
		// --null puts a NUL after the file name: <file>\0<line>:<text>
		file, rest, ok := strings.Cut(l, "\x00")
		if !ok {
			continue
		}
		num, text, ok := strings.Cut(rest, ":")
		n, err := strconv.Atoi(num)
		if !ok || err != nil {
			continue
		}
		v := r.virtual(file)
		if re != nil {
			if g, _ := GlobRegexp(glob); !matchGlob(g, glob, dir, v) {
				continue
			}
		}
		out = append(out, GrepMatch{Path: v, Line: n, Text: text})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
