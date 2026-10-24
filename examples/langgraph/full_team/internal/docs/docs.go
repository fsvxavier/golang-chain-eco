// Package docs gives the PO one write tool, project_doc_write, limited to the
// markdown directories a demand names (doc_roots). No directory named, no write.
package docs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/team"
)

// maxDocBytes caps one write.
const maxDocBytes = 256 << 10

// DocsConfig bounds project_doc_write. Zero values take the defaults.
type DocsConfig struct {
	WorkspaceRoot string // every doc root must lie under it (default WORKSPACE_ROOT, else the cwd)
}

type docArgs struct {
	Path    string `json:"path" jsonschema:"markdown file inside a document directory named by the demand; relative to the first one, e.g. plans/plan.md or next_steps.md"`
	Content string `json:"content" jsonschema:"the text to write"`
	Mode    string `json:"mode,omitempty" jsonschema:"replace (default) or append"`
}

// DocTools is a team.LocalOptions.RoleTools that gives the po role, and only
// it, project_doc_write: the one place the PO can write, so it can keep the
// documentation, the plan and the next steps of the project current. Where is
// named by the demand (its doc_roots, from the hub by the job's exact thread);
// a demand that names none gives no write access at all. Writes are limited to
// .md files inside those directories, which must lie under the workspace
// (symlinks resolved). Source code stays out of reach.
func DocTools(hub func() *team.HubClient, cfg DocsConfig) func(role string) []tools.Tool {
	cfg.WorkspaceRoot = workspaceRoot(cfg.WorkspaceRoot)
	ts := []tools.Tool{
		tools.New("project_doc_write", "Create or update a project document (markdown) such as the documentation, the plan or next_steps. Writes only .md files inside the document directories the demand named; replace overwrites the file, append adds to its end. Keep what is already true; never paste secrets.",
			func(ctx context.Context, a docArgs) (any, error) {
				_, sc, err := team.RequestScopeFromContext(ctx, hub())
				if err != nil || len(sc.DocRoots) == 0 {
					return nil, tools.Errorf("project_doc_write: the demand named no document directory (doc_roots): write refused")
				}
				return writeDocTool(cfg, sc.DocRoots, a)
			}),
	}
	return func(role string) []tools.Tool {
		if role == "po" {
			return ts
		}
		return nil
	}
}

func writeDocTool(cfg DocsConfig, roots []string, a docArgs) (any, error) {
	p, err := docPath(cfg.WorkspaceRoot, roots, a.Path)
	if err != nil {
		return nil, tools.Errorf("project_doc_write: %v", err)
	}
	if len(a.Content) > maxDocBytes {
		return nil, tools.Errorf("project_doc_write: content is %d bytes, the limit is %d", len(a.Content), maxDocBytes)
	}
	switch a.Mode {
	case "", "replace", "append":
	default:
		return nil, tools.Errorf("project_doc_write: mode must be replace or append, got %q", a.Mode)
	}
	if err := writeDoc(p, a.Content, a.Mode == "append"); err != nil {
		return nil, tools.Errorf("project_doc_write: %v", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s (%s)", len(a.Content), a.Path, map[bool]string{true: "appended", false: "replaced"}[a.Mode == "append"]), nil
}

// docPath returns the absolute, symlink-resolved target for p: a .md file
// inside one of roots, each of which must lie under the workspace. A relative
// p starts at the first root. Directories that do not exist yet are allowed
// inside a root; they are created on write.
func docPath(workspace string, roots []string, p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path is required")
	}
	if !strings.EqualFold(filepath.Ext(p), ".md") {
		return "", fmt.Errorf("only .md files can be written, got %q", p)
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return "", fmt.Errorf("%q: '..' is not allowed", p)
		}
	}
	ws, err := resolved(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace root: %v", err)
	}
	var allowed []string
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue // an empty root grants nothing
		}
		if !filepath.IsAbs(r) {
			r = filepath.Join(ws, r)
		}
		if rr, err := resolveExisting(filepath.Clean(r)); err == nil && within(rr, ws) {
			allowed = append(allowed, rr)
		}
	}
	if len(allowed) == 0 {
		return "", fmt.Errorf("no document directory lies inside the workspace: write refused")
	}
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(allowed[0], p)
	}
	dir, base := filepath.Split(filepath.Clean(abs))
	// Resolve the deepest existing ancestor, so a symlinked directory cannot
	// carry the write out of its root.
	real, err := resolveExisting(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	target := filepath.Join(real, base)
	for _, r := range allowed {
		if within(real, r) {
			if fi, err := os.Lstat(target); err == nil && !fi.Mode().IsRegular() {
				return "", fmt.Errorf("%s is not a regular file", p)
			}
			return target, nil
		}
	}
	return "", fmt.Errorf("%s is outside the document directories: write refused", p)
}

// resolveExisting resolves symlinks in the longest existing prefix of p and
// keeps the rest (directories not created yet) as is.
func resolveExisting(p string) (string, error) {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", fmt.Errorf("no existing directory above %s", p)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

func writeDoc(p, content string, appendTo bool) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if appendTo {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".doc-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// workspaceRoot is ws, else WORKSPACE_ROOT, else the working directory.
func workspaceRoot(ws string) string {
	if ws == "" {
		ws = os.Getenv("WORKSPACE_ROOT")
	}
	if ws == "" {
		ws, _ = os.Getwd()
	}
	return ws
}

// resolved returns the absolute path with every symlink resolved.
func resolved(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// within reports whether p is base or below it (both already resolved).
func within(p, base string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
