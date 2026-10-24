package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// outsideWorkspace returns the first token of command that is refused, or "".
// The command is tokenised once, quote aware, and split into segments only on
// ; & | newline outside quotes; every token of every segment is checked.
// Refused: a token (a bare name too, it may be a symlink), the value after "="
// of a flag or VAR=value word, or the text attached to a short flag, that names
// an existing path outside dir (absolute, .., symlink); anything starting with
// "~"; a glob (* ? [) when ANY of its matches is outside dir (a glob with no
// match passes; globs are checked even inside quotes, which is conservative);
// every token after a standalone "--" is a path; and flags that read files named
// elsewhere (see deniedFlag). A token that does not exist on disk (a grep
// pattern such as /api/coupon) cannot read anything and passes.
// The caller rejects \ { } < > $ and backticks first, so bash cannot rewrite a
// token. Demo guard, not a sandbox: other flags are not inspected.
func outsideWorkspace(dir, command string) string {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		root = dir
	}
	// bad reports whether p, or any glob match of p, lies outside root.
	bad := func(p string) bool {
		if strings.HasPrefix(p, "~") {
			return true
		}
		if !filepath.IsAbs(p) {
			p = dir + "/" + p // not Join: keep "link/.." for the OS to resolve
		}
		cands := []string{p, filepath.Clean(p)}
		if strings.ContainsAny(p, "*?[") {
			m, _ := filepath.Glob(p)
			cands = append(cands, m...)
		}
		for _, c := range cands {
			if _, err := os.Lstat(c); err != nil {
				continue
			}
			real, err := filepath.EvalSymlinks(c)
			if err != nil {
				return true
			}
			if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	for _, toks := range tokenize(command) {
		prog, started, afterArg, paths := "", false, false, false
		for _, tok := range toks {
			if !started && assignRe.MatchString(tok) { // VAR=value before the program
				if _, v, _ := strings.Cut(tok, "="); bad(v) {
					return tok
				}
				continue
			}
			isFlag := !paths && strings.HasPrefix(tok, "-") && tok != "-"
			if !isFlag || !started {
				if !started {
					prog = filepath.Base(tok)
				} else {
					afterArg = true
				}
				started = true
				if tok != "" && bad(tok) {
					return tok
				}
				continue
			}
			if tok == "--" {
				paths = true
				continue
			}
			if deniedFlag(prog, tok, afterArg) {
				return tok
			}
			if _, v, ok := strings.Cut(tok, "="); ok && v != "" && bad(v) {
				return tok
			}
			if !strings.HasPrefix(tok, "--") && len(tok) > 2 && bad(tok[2:]) { // -fPATH
				return tok
			}
		}
	}
	return ""
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// grepAllowed are long options that are exact prefixes of a denied one and
// must keep working.
var grepAllowed = []string{"--include", "--exclude"}

// deniedFlag reports flags that read a file named elsewhere or move the program
// to another directory. Long options are matched by prefix (getopt accepts any
// unambiguous one: --deref, --fil), short ones by letter inside an all-letter
// cluster (-nf, -rR). grep -efoo is refused too: it cannot be told from -f.
// For go and git, -C is denied only before the subcommand (git diff -C is
// copy detection); an attached -C/path is caught by the path check.
func deniedFlag(prog, flag string, afterArg bool) bool {
	var long []string
	switch prog {
	case "grep":
		long = []string{"--file", "--exclude-from", "--include-from", "--dereference-recursive"}
	case "git", "go":
		if flag == "-C" && !afterArg {
			return true
		}
		long = []string{"--git-dir", "--work-tree"}
	default:
		return false
	}
	if strings.HasPrefix(flag, "--") {
		name, _, _ := strings.Cut(flag, "=")
		if slices.Contains(grepAllowed, name) || name == "--" {
			return false
		}
		return slices.ContainsFunc(long, func(l string) bool { return strings.HasPrefix(l, name) })
	}
	if prog != "grep" {
		return false
	}
	letters := strings.TrimPrefix(flag, "-")
	for _, r := range letters {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return strings.HasPrefix(letters, "f") // -f/etc/x
		}
	}
	return strings.ContainsAny(letters, "fR")
}

// tokenize splits s into segments (on ; & | newline) of tokens (on blanks),
// honouring single and double quotes: separators inside quotes are plain text.
func tokenize(s string) [][]string {
	var segs [][]string
	var toks []string
	var cur strings.Builder
	var quote rune
	started := false
	endTok := func() {
		if started {
			toks = append(toks, cur.String())
			cur.Reset()
			started = false
		}
	}
	endSeg := func() {
		endTok()
		if len(toks) > 0 {
			segs = append(segs, toks)
			toks = nil
		}
	}
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
			endTok()
		case r == ';' || r == '&' || r == '|' || r == '\n':
			endSeg()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	endSeg()
	return segs
}
