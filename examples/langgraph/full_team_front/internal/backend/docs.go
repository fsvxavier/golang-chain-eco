package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// maxDocBytes caps one write.
const maxDocBytes = 256 << 10

// DocsConfig bounds project_doc_write. Zero values take the defaults.
type DocsConfig struct {
	WorkspaceRoot string   // default WORKSPACE_ROOT, else the cwd
	Dirs          []string // writable directories under the workspace (default PO_DOCS_DIRS, else "project")
}

func (c DocsConfig) withDefaults() DocsConfig {
	c.WorkspaceRoot = Config{WorkspaceRoot: c.WorkspaceRoot}.withDefaults().WorkspaceRoot
	if len(c.Dirs) == 0 {
		for _, d := range strings.Split(os.Getenv("PO_DOCS_DIRS"), ",") {
			if d = strings.TrimSpace(d); d != "" {
				c.Dirs = append(c.Dirs, d)
			}
		}
	}
	if len(c.Dirs) == 0 {
		c.Dirs = []string{"project"}
	}
	return c
}

type docArgs struct {
	Path    string `json:"path" jsonschema:"markdown file under the project docs directory, e.g. project/front-end/plans/plan.md or project/front-end/next_steps.md"`
	Content string `json:"content" jsonschema:"the text to write"`
	Mode    string `json:"mode,omitempty" jsonschema:"replace (default) or append"`
}

// DocTools is a team.LocalOptions.RoleTools that gives the po role, and only
// it, project_doc_write: the one place the PO can write, so it can keep the
// documentation, the plan and the next steps of the project current. Source
// code stays out of reach: writes are limited to .md files under cfg.Dirs
// (inside the workspace, symlinks resolved).
func DocTools(cfg DocsConfig) func(role string) []tools.Tool {
	cfg = cfg.withDefaults()
	ts := []tools.Tool{
		tools.New("project_doc_write", "Create or update a project document (markdown) such as the documentation, the plan or next_steps. Writes only .md files under the project docs directory; replace overwrites the file, append adds to its end. Keep what is already true; never paste secrets.",
			func(ctx context.Context, a docArgs) (any, error) {
				p, err := docPath(cfg, a.Path)
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
			}),
	}
	return func(role string) []tools.Tool {
		if role == "po" {
			return ts
		}
		return nil
	}
}

// docPath returns the absolute, symlink-resolved target for p, refusing
// anything but a .md file under one of cfg.Dirs of the workspace.
func docPath(cfg DocsConfig, p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path is required")
	}
	if !strings.EqualFold(filepath.Ext(p), ".md") {
		return "", fmt.Errorf("only .md files can be written, got %q", p)
	}
	ws, err := resolved(cfg.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(ws, p)
	}
	abs = filepath.Clean(abs)
	// Resolve the deepest existing ancestor, so a symlinked directory cannot
	// carry the write out of the allowed directory.
	dir, base := filepath.Split(abs)
	real, err := resolveExisting(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	target := filepath.Join(real, base)
	for _, d := range cfg.Dirs {
		allowed, err := resolveExisting(filepath.Join(ws, d))
		if err == nil && within(real, allowed) && within(allowed, ws) {
			if fi, err := os.Lstat(target); err == nil && !fi.Mode().IsRegular() {
				return "", fmt.Errorf("%s is not a regular file", p)
			}
			return target, nil
		}
	}
	return "", fmt.Errorf("%s is outside the writable directories %v", p, cfg.Dirs)
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
