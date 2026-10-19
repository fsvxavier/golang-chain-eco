package guards

import (
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// maxScriptDepth bounds script-in-script inspection (a script that runs a
// script that runs a script...).
const maxScriptDepth = 3

// interpreters run the file named by their first non-flag argument.
var interpreters = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "source": true, ".": true,
	"python": true, "python3": true, "node": true, "perl": true, "ruby": true}

// redirect matches a word that carries an output redirection: `>f`, `>>f`,
// `2>f`, or a bare `>` / `>>` whose target is the next word.
var redirect = regexp.MustCompile(`^(?:\d|&)?(>>?)\|?(.*)$`)

// heredocMarker matches `<<EOF`, `<<-EOF`, `<<'EOF'`.
var heredocMarker = regexp.MustCompile(`<<-?\s*['"]?(\w+)['"]?`)

// fetchToShell is the obvious obfuscation: a download or a decoder piped into
// an interpreter, or an interpreter fed by a command substitution of one.
var fetchToShell = regexp.MustCompile(`\b(?:base64|curl|wget)\b[^\n;&]*\|\s*(?:sudo\s+)?(?:\S*/)?(?:sh|bash|zsh|dash|python3?|node|perl)\b` +
	`|\b(?:sh|bash|zsh|dash)\s+<\(\s*(?:curl|wget)\b` +
	`|\b(?:sh|bash|zsh|dash)\b.*\$\(\s*(?:curl|wget|base64)\b`)

type writeKind int

const (
	kindSet writeKind = iota
	kindAppend
	kindEdit
)

// fileWrite is one change the agent makes to a file.
type fileWrite struct {
	path    string
	content string
	kind    writeKind
	old     string // kindEdit: the replaced text
	all     bool   // kindEdit: replace_all
}

// scriptBook remembers, per thread (run), the last content the agent wrote to
// each file, so that running the file later can be inspected.
// ponytail: process-wide map, entries are never evicted; add a run-end hook
// when the dispatcher can tell a run finished.
type scriptBook struct {
	mu    sync.Mutex
	files map[string]map[string]string // thread -> normalised path -> content
}

var scripts scriptBook

func normPath(p string) string {
	return strings.TrimLeft(path.Clean(p), "/")
}

func applyWrite(m map[string]string, w fileWrite) {
	switch w.kind {
	case kindAppend:
		m[w.path] = m[w.path] + "\n" + w.content
	case kindEdit:
		prev, ok := m[w.path]
		if ok && w.old != "" && strings.Contains(prev, w.old) {
			n := 1
			if w.all {
				n = -1
			}
			m[w.path] = strings.Replace(prev, w.old, w.content, n)
		} else {
			// base unknown: keep everything seen so far
			m[w.path] = prev + "\n" + w.content
		}
	default:
		m[w.path] = w.content
	}
}

func (b *scriptBook) commit(thread string, ws []fileWrite) {
	if len(ws) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.files == nil {
		b.files = map[string]map[string]string{}
	}
	m := b.files[thread]
	if m == nil {
		m = map[string]string{}
		b.files[thread] = m
	}
	for _, w := range ws {
		applyWrite(m, w)
	}
}

// overlay returns the thread's files with ws applied on top, without storing it.
func (b *scriptBook) overlay(thread string, ws []fileWrite) map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]string, len(b.files[thread])+len(ws))
	for k, v := range b.files[thread] {
		out[k] = v
	}
	for _, w := range ws {
		applyWrite(out, w)
	}
	return out
}

// writeTargets splits a command into its output-redirection targets and the
// remaining words. Input redirections (`<f`, `<<EOF`, `<<<x`) are dropped.
func writeTargets(c Command) (targets []fileWrite, words []string) {
	for j := 0; j < len(c); j++ {
		m := redirect.FindStringSubmatch(c[j])
		if m == nil {
			if !strings.HasPrefix(c[j], "<") {
				words = append(words, c[j])
			}
			continue
		}
		p := m[2]
		if p == "" && j+1 < len(c) {
			j++
			p = c[j]
		}
		if p != "" && !strings.HasPrefix(p, "&") && p != "/dev/null" {
			targets = append(targets, fileWrite{path: normPath(p), kind: kindSet})
			if m[1] == ">>" {
				targets[len(targets)-1].kind = kindAppend
			}
		}
	}
	return targets, words
}

// heredocBody returns the body of the first heredoc in line.
func heredocBody(line string) (string, bool) {
	loc := heredocMarker.FindStringSubmatch(line)
	if loc == nil {
		return "", false
	}
	delim := loc[1]
	lines := strings.Split(line, "\n")
	for i, l := range lines {
		if !heredocMarker.MatchString(l) {
			continue
		}
		var body []string
		for _, b := range lines[i+1:] {
			if strings.TrimLeft(b, "\t") == delim {
				break
			}
			body = append(body, b)
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// shellWrites returns the files a shell line writes and what it writes:
// `> f`, `>> f`, `tee f`, with the content taken from the command's arguments
// (echo, printf) or from a heredoc body. Content produced by any other command
// is unknown and not recorded.
func shellWrites(line string, cmds []Command) []fileWrite {
	body, hasBody := heredocBody(line)
	var out []fileWrite
	var prevArgs []string
	for _, c := range cmds {
		targets, words := writeTargets(c)
		var args []string
		if len(words) > 1 {
			args = words[1:]
		}
		if len(words) > 0 && baseName(words[0]) == "tee" {
			app := false
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					app = app || strings.Contains(a, "a")
					continue
				}
				kind := kindSet
				if app {
					kind = kindAppend
				}
				targets = append(targets, fileWrite{path: normPath(a), kind: kind})
			}
			args = prevArgs
		}
		content := strings.Join(args, "\n")
		if hasBody {
			content = body
		}
		if strings.TrimSpace(content) != "" {
			for _, t := range targets {
				t.content = content
				out = append(out, t)
			}
		}
		prevArgs = args
	}
	return out
}

// toolWrites returns the file change of write_file / edit_file calls.
func toolWrites(call messages.ToolCall) []fileWrite {
	p, _ := call.Args["file_path"].(string)
	if p == "" {
		return nil
	}
	p = normPath(p)
	switch call.Name {
	case "write_file":
		c, _ := call.Args["content"].(string)
		return []fileWrite{{path: p, content: c, kind: kindSet}}
	case "edit_file":
		old, _ := call.Args["old_string"].(string)
		nw, _ := call.Args["new_string"].(string)
		all, _ := call.Args["replace_all"].(bool)
		return []fileWrite{{path: p, content: nw, kind: kindEdit, old: old, all: all}}
	}
	return nil
}

// rememberWrites records what a call that passed the guards writes.
func rememberWrites(thread string, call messages.ToolCall, shellTools map[string][]string) {
	ws := toolWrites(call)
	for _, line := range shellLines(call, shellTools) {
		cmds, _ := Commands(line)
		ws = append(ws, shellWrites(line, cmds)...)
	}
	scripts.commit(thread, ws)
}

// scriptRefs returns the file a command would execute, if any.
func scriptRefs(c Command) []string {
	if len(c) == 0 {
		return nil
	}
	if interpreters[c.Executable()] {
		for _, a := range c.Args() {
			if !strings.HasPrefix(a, "-") {
				return []string{a}
			}
		}
		return nil
	}
	if strings.Contains(c[0], "/") {
		return []string{c[0]}
	}
	return nil
}

// lookup finds the last content written to ref. A bare name (no directory)
// matches any written file with that base name; all matches are joined so the
// inspection stays conservative.
func lookup(files map[string]string, ref string) (string, bool) {
	if c, ok := files[normPath(ref)]; ok {
		return c, true
	}
	if strings.Contains(ref, "/") {
		return "", false
	}
	var hits []string
	for p, c := range files {
		if path.Base(p) == ref {
			hits = append(hits, c)
		}
	}
	if len(hits) == 0 {
		return "", false
	}
	return strings.Join(hits, "\n"), true
}

// rule decides a guard's own policy over the simple commands of one line.
// It returns a reason, or "".
type rule func(cmds []Command, unsafe bool, line string) string

// lineViolation applies the download-to-shell rule, the guard's rule, and the
// rule again to the content of every file the line runs (written earlier in
// the thread, or in this same line).
func lineViolation(thread, line string, depth int, r rule) string {
	if fetchToShell.MatchString(line) {
		return "pipes a download or a decoder into a shell"
	}
	cmds, unsafe := Commands(line)
	if why := r(cmds, unsafe, line); why != "" {
		return why
	}
	var files map[string]string
	for _, c := range cmds {
		for _, ref := range scriptRefs(c) {
			if files == nil {
				files = scripts.overlay(thread, shellWrites(line, cmds))
			}
			content, ok := lookup(files, ref)
			if !ok {
				continue
			}
			if depth >= maxScriptDepth {
				return "runs " + ref + ", nested too deeply to inspect"
			}
			if why := lineViolation(thread, content, depth+1, r); why != "" {
				return "runs " + ref + " (written earlier), which " + why
			}
		}
	}
	return ""
}
