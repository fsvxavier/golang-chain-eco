package main

import (
	"os"
	"path/filepath"
	"strings"
)

// outsideWorkspace returns the first argument of command that names an
// existing path outside dir, or "~..." (home expansion). Arguments that do not
// exist on disk (grep patterns, new names) cannot read anything and pass.
// shortcut: flags are skipped whole, so "--include=/etc" is not inspected, and
// the tokenizer knows quotes but no backslash escapes; both are covered by the
// program allowlist (ls, grep, echo and offline go/git take no file-reading flag
// that matters here), upgrade to a real shell parser if programs are added.
func outsideWorkspace(dir, command string) string {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		root = dir
	}
	for _, seg := range strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune(";&|\n", r) }) {
		for _, tok := range tokenize(seg) {
			if tok == "" || strings.HasPrefix(tok, "-") {
				continue
			}
			if strings.HasPrefix(tok, "~") {
				return tok
			}
			p := tok
			if !filepath.IsAbs(p) {
				p = filepath.Join(dir, p)
			}
			p = filepath.Clean(p)
			if _, err := os.Lstat(p); err != nil {
				continue
			}
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return tok
			}
			if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return tok
			}
		}
	}
	return ""
}

// tokenize splits on whitespace, honouring single and double quotes.
func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}
