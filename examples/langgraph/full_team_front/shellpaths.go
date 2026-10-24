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
// "~"; a glob (* ? [): a path-like one (slash, or starting with "." or "~") is
// rejected when it holds "[" at all, when its literal prefix (the components
// before the first wildcard one) is not inside dir, or when ".." follows a
// wildcard; any glob is also rejected when a match of it or of its Clean form is
// outside dir. Bare patterns (grep '[0-9]') and workspace globs (src/*.tsx,
// ls *) work; a glob with no match passes; globs are checked inside quotes too;
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
		tok := p
		if strings.HasPrefix(p, "~") {
			return true
		}
		if !filepath.IsAbs(p) {
			p = dir + "/" + p // not Join: keep "link/.." for the OS to resolve
		}
		cands := []string{p, filepath.Clean(p)}
		if strings.ContainsAny(tok, "*?[") {
			if pathLike(tok) && badGlob(dir, root, tok) {
				return true
			}
			if dotGlob(tok) {
				return true
			}
			gs := []string{p, filepath.Clean(p)} // Clean covers "x/*/", "x/*/." and "x/*//"
			if strings.Contains(p, "[") {        // bash and Go read brackets differently: also try a superset
				sp := bracketsToAny(p)
				gs = append(gs, sp, filepath.Clean(sp))
			}
			for _, g := range gs {
				m, _ := filepath.Glob(g)
				cands = append(cands, m...)
			}
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

// dotGlob reports a wildcard component that starts with "." and can match "."
// or "..": bash before 5.2 (globskipdots off) expands .* and .? to "..".
// shortcut: this also refuses a bare grep regex such as '.*'; use another form.
func dotGlob(tok string) bool {
	for _, c := range strings.Split(tok, "/") {
		if strings.HasPrefix(c, ".") && strings.ContainsAny(c, "*?[") {
			if ok1, _ := filepath.Match(c, "."); ok1 {
				return true
			}
			if ok2, _ := filepath.Match(c, ".."); ok2 {
				return true
			}
		}
	}
	return false
}

// bracketsToAny replaces every bracket expression with "?": in both dialects
// it matches exactly one character, so the copy matches a superset. An
// unterminated "[" is literal in bash and left as is.
func bracketsToAny(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		if j < len(s) && (s[j] == '!' || s[j] == '^') {
			j++
		}
		if j < len(s) && s[j] == ']' {
			j++
		}
		for j < len(s) && s[j] != ']' {
			if s[j] == '[' && j+1 < len(s) && strings.IndexByte(":.=", s[j+1]) >= 0 {
				if k := strings.Index(s[j+2:], string(s[j+1])+"]"); k >= 0 {
					j += k + 4
					continue
				}
			}
			j++
		}
		if j >= len(s) {
			b.WriteByte('[')
			continue
		}
		b.WriteByte('?')
		i = j
	}
	return b.String()
}

// pathLike: a token with a slash, or starting with "." or "~", names a path;
// a bare word is a grep pattern or a workspace-relative name.
func pathLike(tok string) bool {
	return strings.Contains(tok, "/") || strings.HasPrefix(tok, ".") || strings.HasPrefix(tok, "~")
}

// badGlob decides a path-like glob structurally, not by Go's matches (bash
// reads [!x], []x] and [[:alpha:]] differently; bare globs are checked through
// a superset instead, see bracketsToAny): it is rejected
// when it holds "[", when the literal prefix before the first wildcard
// component is not inside the workspace, or when ".." follows a wildcard.
func badGlob(dir, root, tok string) bool {
	if strings.Contains(tok, "[") {
		return true
	}
	comps := strings.Split(tok, "/")
	n := slices.IndexFunc(comps, func(c string) bool { return strings.ContainsAny(c, "*?") })
	if n < 0 {
		return false
	}
	if slices.Contains(comps[n:], "..") {
		return true
	}
	lit := strings.Join(comps[:n], "/")
	if filepath.IsAbs(tok) {
		lit = "/" + lit
	} else {
		lit = dir + "/" + lit
	}
	lit = filepath.Clean(lit)
	if _, err := os.Lstat(lit); err == nil {
		real, err := filepath.EvalSymlinks(lit)
		return err != nil || !within(root, real)
	}
	return !within(root, lit) && !within(filepath.Clean(dir), lit)
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
