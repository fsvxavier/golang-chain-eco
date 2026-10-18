// Package middleware holds the Deep Agents middleware stack
// (deepagents.middleware): filesystem tools, subagents, memory, skills and
// tool-call patching.
package middleware

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

const filesystemPrompt = `## Filesystem Tools ` + "`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`" + `

You have access to a virtual filesystem. All paths must be absolute (start with "/").
- ls: list files in a directory
- read_file: read a file (use offset/limit for large files)
- write_file: create a new file
- edit_file: exact string replacement in an existing file
- glob: find files by pattern
- grep: literal text search across files`

const (
	defaultReadLimit = 100
	maxLineLen       = 2000
	emptyFileWarning = "System reminder: File exists but has empty contents"
)

// Filesystem adds the filesystem tools (and execute for sandbox backends).
type Filesystem struct {
	Backend backends.Backend
	// SystemPrompt overrides the default tool guidance.
	SystemPrompt string
}

func (Filesystem) Name() string { return "filesystem" }
func (Filesystem) StateSchema() graph.Schema {
	return graph.Schema{"files": backends.FilesChannel()}
}

func (f Filesystem) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	p := f.SystemPrompt
	if p == "" {
		p = filesystemPrompt
		if _, ok := backends.AsExecutor(f.Backend); ok {
			p += "\n- execute: run a shell command"
		}
	}
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n" + p)
	return next(ctx, &r)
}

// result builds the tool return: plain text, or a Command carrying a files
// update plus the ToolMessage.
func result(ctx context.Context, name, text string, upd backends.FilesUpdate) (any, error) {
	if upd == nil {
		return text, nil
	}
	m := messages.Tool(text, tools.CallID(ctx))
	m.Name = name
	return graph.Command{Update: graph.State{"files": upd, "messages": []messages.Message{m}}}, nil
}

func (f Filesystem) Tools() []tools.Tool {
	b := f.Backend
	type lsArgs struct {
		Path string `json:"path" description:"Absolute directory path"`
	}
	type readArgs struct {
		FilePath string `json:"file_path" description:"Absolute file path"`
		Offset   int    `json:"offset,omitempty" description:"Line to start from (0-based)"`
		Limit    int    `json:"limit,omitempty" description:"Max lines to read (default 100)"`
	}
	type writeArgs struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	type editArgs struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all,omitempty"`
	}
	type globArgs struct {
		Pattern string `json:"pattern" description:"Glob pattern, e.g. **/*.go"`
		Path    string `json:"path,omitempty" description:"Directory to search (default /)"`
	}
	type grepArgs struct {
		Pattern    string `json:"pattern" description:"Literal text to search for (not regex)"`
		Path       string `json:"path,omitempty"`
		Glob       string `json:"glob,omitempty" description:"Only search files matching this glob"`
		OutputMode string `json:"output_mode,omitempty" description:"files_with_matches (default), content or count"`
	}
	ts := []tools.Tool{
		tools.New("ls", "Lists all files in a directory.", func(ctx context.Context, a lsArgs) (any, error) {
			infos, err := b.Ls(ctx, orRoot(a.Path))
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if len(infos) == 0 {
				return fmt.Sprintf("No files found in %s", orRoot(a.Path)), nil
			}
			paths := make([]string, len(infos))
			for i, fi := range infos {
				paths[i] = fi.Path
			}
			return strings.Join(paths, "\n"), nil
		}),
		tools.New("read_file", "Reads a file with line numbers. Use offset/limit to page through large files.", func(ctx context.Context, a readArgs) (any, error) {
			c, err := b.Read(ctx, a.FilePath)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if strings.TrimSpace(c) == "" {
				return emptyFileWarning, nil
			}
			return numbered(c, a.Offset, orInt(a.Limit, defaultReadLimit)), nil
		}),
		tools.New("write_file", "Writes content to a new file. Fails if the file already exists.", func(ctx context.Context, a writeArgs) (any, error) {
			upd, err := b.Write(ctx, a.FilePath, a.Content)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			return result(ctx, "write_file", "Updated file "+backends.Clean(a.FilePath), upd)
		}),
		tools.New("edit_file", "Performs exact string replacement in a file. old_string must be unique unless replace_all is true.", func(ctx context.Context, a editArgs) (any, error) {
			upd, n, err := b.Edit(ctx, a.FilePath, a.OldString, a.NewString, a.ReplaceAll)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			return result(ctx, "edit_file", fmt.Sprintf("Successfully replaced %d instance(s) of the string in '%s'", n, backends.Clean(a.FilePath)), upd)
		}),
		tools.New("glob", "Find files matching a glob pattern, returning absolute paths.", func(ctx context.Context, a globArgs) (any, error) {
			infos, err := b.Glob(ctx, a.Pattern, orRoot(a.Path))
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if len(infos) == 0 {
				return "No files matched", nil
			}
			paths := make([]string, len(infos))
			for i, fi := range infos {
				paths[i] = fi.Path
			}
			return strings.Join(paths, "\n"), nil
		}),
		tools.New("grep", "Search for a LITERAL text pattern across files (NOT regex).", func(ctx context.Context, a grepArgs) (any, error) {
			ms, err := b.Grep(ctx, a.Pattern, orRoot(a.Path), a.Glob)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			return formatGrep(ms, a.OutputMode), nil
		}),
	}
	if ex, ok := backends.AsExecutor(b); ok {
		type execArgs struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout,omitempty" description:"Seconds (default 120)"`
		}
		ts = append(ts, tools.New("execute", "Executes a shell command in the sandbox and returns its output and exit code.", func(ctx context.Context, a execArgs) (any, error) {
			r, err := ex.Execute(ctx, a.Command, time.Duration(orInt(a.Timeout, 120))*time.Second)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			out := r.Output
			if r.Truncated {
				out += "\n[output truncated]"
			}
			return fmt.Sprintf("%s\n[exit code: %d]", out, r.ExitCode), nil
		}))
	}
	return ts
}

func orRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func orInt(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

func numbered(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	if offset >= len(lines) {
		return fmt.Sprintf("Error: line offset %d exceeds file length (%d lines)", offset, len(lines))
	}
	end := min(offset+limit, len(lines))
	var b strings.Builder
	for i := offset; i < end; i++ {
		l := lines[i]
		if len(l) > maxLineLen {
			l = l[:maxLineLen] + "…"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, l)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "[%d more lines; use offset=%d to continue]\n", len(lines)-end, end)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatGrep(ms []backends.GrepMatch, mode string) string {
	if len(ms) == 0 {
		return "No matches found"
	}
	switch mode {
	case "content":
		var b strings.Builder
		for _, m := range ms {
			fmt.Fprintf(&b, "%s:%d:%s\n", m.Path, m.Line, m.Text)
		}
		return strings.TrimRight(b.String(), "\n")
	case "count":
		counts := map[string]int{}
		for _, m := range ms {
			counts[m.Path]++
		}
		var b strings.Builder
		for _, p := range sortedKeys(counts) {
			fmt.Fprintf(&b, "%s:%d\n", p, counts[p])
		}
		return strings.TrimRight(b.String(), "\n")
	}
	seen := map[string]bool{}
	for _, m := range ms {
		seen[m.Path] = true
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n")
}

func sortedKeys(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
