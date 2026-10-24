package main

import (
	"os"
	"path/filepath"
	"strings"
)

// outsideWorkspace returns the first argument of command that is refused, or "".
// Refused: a path argument (or the value of a --flag=value, or the text attached
// to a short flag) that names an existing path outside dir, anything starting
// with "~", every token after a standalone "--" treated as a path, flags that
// read files named elsewhere (grep -f/--file/--exclude-from/--include-from and
// -R/--dereference-recursive, go/git -C/--git-dir/--work-tree). A token that does
// not exist on disk (a grep pattern, a new name) cannot read anything and passes.
// shortcut: the tokenizer knows quotes only; the caller rejects \ { } < > $ and
// backticks first so bash cannot rewrite a token. Upgrade to a real shell parser
// if programs with other file-taking flags are added to the allowlist.
func outsideWorkspace(dir, command string) string {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		root = dir
	}
	bad := func(p string) bool {
		if strings.HasPrefix(p, "~") {
			return true
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		p = filepath.Clean(p)
		if _, err := os.Lstat(p); err != nil {
			return false
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return true
		}
		rel, err := filepath.Rel(root, real)
		return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	for _, seg := range strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune(";&|\n", r) }) {
		toks := tokenize(seg)
		if len(toks) == 0 {
			continue
		}
		prog, paths := filepath.Base(toks[0]), false
		for _, tok := range toks {
			switch {
			case paths || !strings.HasPrefix(tok, "-"):
				if tok != "" && bad(tok) {
					return tok
				}
			case tok == "--":
				paths = true
			case deniedFlag(prog, tok):
				return tok
			default:
				if _, v, ok := strings.Cut(tok, "="); ok && v != "" && bad(v) {
					return tok
				}
				if !strings.HasPrefix(tok, "--") && len(tok) > 2 && bad(tok[2:]) { // -fPATH, -I/etc
					return tok
				}
			}
		}
	}
	return ""
}

// deniedFlag reports flags that make a program read or enter a path the
// argument check cannot attribute to the workspace.
func deniedFlag(prog, tok string) bool {
	name, _, _ := strings.Cut(tok, "=")
	switch prog {
	case "grep":
		switch name {
		case "--file", "--exclude-from", "--include-from", "--dereference-recursive":
			return true
		}
		// a short bundle (-nf, -rR) holding f or R; conservative: -efoo is refused too
		return !strings.HasPrefix(tok, "--") && strings.ContainsAny(tok, "fR")
	case "go", "git":
		return name == "-C" || name == "--git-dir" || name == "--work-tree" || (!strings.HasPrefix(tok, "--") && strings.HasPrefix(tok, "-C"))
	}
	return false
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
