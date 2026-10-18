// Package backends provides the pluggable file storage behind Deep Agents'
// virtual filesystem (deepagents.backends): in graph state, on disk, in a
// LangGraph store, or composed by path prefix.
package backends

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FileData is a stored file.
type FileData struct {
	Content    string
	CreatedAt  time.Time
	ModifiedAt time.Time
}

// FileInfo describes a directory entry or glob hit.
type FileInfo struct {
	Path       string
	IsDir      bool
	Size       int
	ModifiedAt string
}

// GrepMatch is a literal-search hit.
type GrepMatch struct {
	Path string
	Line int
	Text string
}

// FilesUpdate maps path -> new content for backends whose files live in
// graph state; a nil entry deletes. Backends with external storage return
// nil and the tool reports success without a state update.
type FilesUpdate map[string]*FileData

// Backend is the filesystem abstraction used by the filesystem middleware.
// Paths are absolute, slash-separated and virtual (rooted at "/").
type Backend interface {
	Ls(ctx context.Context, dir string) ([]FileInfo, error)
	// Read returns the raw file content.
	Read(ctx context.Context, p string) (string, error)
	// Write creates a new file; it fails when p exists.
	Write(ctx context.Context, p, content string) (FilesUpdate, error)
	// Edit replaces old with new; old must be unique unless all is set.
	Edit(ctx context.Context, p, old, new string, all bool) (FilesUpdate, int, error)
	Glob(ctx context.Context, pattern, dir string) ([]FileInfo, error)
	// Grep searches for a literal string; glob optionally filters paths.
	Grep(ctx context.Context, pattern, dir, glob string) ([]GrepMatch, error)
}

// ExecResult is the outcome of Executor.Execute.
type ExecResult struct {
	Output    string
	ExitCode  int
	Truncated bool
}

// Executor is implemented by sandbox backends that can run shell commands.
type Executor interface {
	Execute(ctx context.Context, command string, timeout time.Duration) (ExecResult, error)
}

// Errors shared by backends.
var (
	ErrNotFound = errors.New("file not found")
	ErrExists   = errors.New("file already exists")
)

// Clean normalises a virtual path to an absolute, cleaned form.
func Clean(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// ApplyEdit performs the shared replace logic.
func ApplyEdit(content, old, new string, all bool) (string, int, error) {
	if old == "" {
		return "", 0, errors.New("old_string must not be empty")
	}
	n := strings.Count(content, old)
	switch {
	case n == 0:
		return "", 0, fmt.Errorf("string not found in file: '%s'", old)
	case n > 1 && !all:
		return "", 0, fmt.Errorf("string '%s' appears %d times in file; use replace_all=true or provide more context", old, n)
	}
	if all {
		return strings.ReplaceAll(content, old, new), n, nil
	}
	return strings.Replace(content, old, new, 1), 1, nil
}

// GlobRegexp converts a glob (supporting **, *, ?, [..] and {a,b}) to a regexp
// matched against full virtual paths.
func GlobRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	brace := 0
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[', ']':
			b.WriteByte(c)
		case '{':
			brace++
			b.WriteString("(?:")
		case '}':
			if brace > 0 {
				brace--
				b.WriteString(")")
			} else {
				b.WriteString(`\}`)
			}
		case ',':
			if brace > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// matchGlob matches the file path relative to dir (or its basename when the
// pattern has no slash) against pattern.
func matchGlob(re *regexp.Regexp, pattern, dir, full string) bool {
	rel := strings.TrimPrefix(strings.TrimPrefix(full, dir), "/")
	if !strings.Contains(pattern, "/") {
		return re.MatchString(path.Base(full))
	}
	return re.MatchString(rel)
}

// lsFromFiles lists the immediate children of dir given a flat file set.
func lsFromFiles(files map[string]FileData, dir string) []FileInfo {
	dir = Clean(dir)
	prefix := strings.TrimSuffix(dir, "/") + "/"
	seen := map[string]FileInfo{}
	for p, f := range files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := p[len(prefix):]
		if i := strings.Index(rest, "/"); i >= 0 {
			d := prefix + rest[:i]
			seen[d] = FileInfo{Path: d + "/", IsDir: true}
			continue
		}
		seen[p] = FileInfo{Path: p, Size: len(f.Content), ModifiedAt: f.ModifiedAt.Format(time.RFC3339)}
	}
	out := make([]FileInfo, 0, len(seen))
	for _, fi := range seen {
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func globFiles(files map[string]FileData, pattern, dir string) ([]FileInfo, error) {
	re, err := GlobRegexp(pattern)
	if err != nil {
		return nil, err
	}
	dir = Clean(dir)
	prefix := strings.TrimSuffix(dir, "/") + "/"
	var out []FileInfo
	for p, f := range files {
		if strings.HasPrefix(p, prefix) && matchGlob(re, pattern, dir, p) {
			out = append(out, FileInfo{Path: p, Size: len(f.Content), ModifiedAt: f.ModifiedAt.Format(time.RFC3339)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func grepFiles(files map[string]FileData, pattern, dir, glob string) ([]GrepMatch, error) {
	var re *regexp.Regexp
	if glob != "" {
		var err error
		if re, err = GlobRegexp(glob); err != nil {
			return nil, err
		}
	}
	dir = Clean(dir)
	prefix := strings.TrimSuffix(dir, "/") + "/"
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []GrepMatch
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) || (re != nil && !matchGlob(re, glob, dir, p)) {
			continue
		}
		for i, line := range strings.Split(files[p].Content, "\n") {
			if strings.Contains(line, pattern) {
				out = append(out, GrepMatch{Path: p, Line: i + 1, Text: line})
			}
		}
	}
	return out, nil
}

// AsExecutor returns b's Executor, looking through Composite defaults.
func AsExecutor(b Backend) (Executor, bool) {
	if c, ok := b.(Composite); ok {
		return AsExecutor(c.Default)
	}
	if p, ok := b.(Permissioned); ok {
		if _, ok := AsExecutor(p.Backend); !ok {
			return nil, false
		}
		return p, true
	}
	e, ok := b.(Executor)
	return e, ok
}
