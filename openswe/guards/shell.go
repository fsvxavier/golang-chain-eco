// Package guards holds the agent middleware that keeps an autonomous coding
// agent inside its rules: pull requests only through the audited tool,
// workflow-file pushes only with approval, tool failures that do not kill the
// run, interrupted tool calls that do not wedge a thread, and a circuit breaker
// for a sandbox that stopped answering.
package guards

import (
	"strings"
)

// Command is one simple shell command: its words after redirections and
// leading VAR=value assignments / wrapper commands (env, sudo, time...).
type Command []string

// maxShellDepth bounds nested `bash -c "..."` expansion.
const maxShellDepth = 3

var wrappers = map[string]bool{"env": true, "sudo": true, "command": true, "exec": true, "time": true, "nice": true, "nohup": true, "stdbuf": true, "timeout": true}

var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true}

// splitWords tokenises a command line with POSIX-ish quoting and returns
// commands split on ; && || | & and newlines. Command substitution and
// backticks are not evaluated; Unsafe reports their presence.
func splitWords(line string) (cmds []Command, unsafe bool) {
	var cur Command
	var word strings.Builder
	inWord := false
	var quote rune
	flushWord := func() {
		if inWord {
			cur = append(cur, word.String())
			word.Reset()
			inWord = false
		}
	}
	flushCmd := func() {
		flushWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
			cur = nil
		}
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
				i++
				word.WriteRune(rs[i])
			default:
				if r == '`' || (r == '$' && i+1 < len(rs) && rs[i+1] == '(') {
					unsafe = true
				}
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && i+1 < len(rs) && rs[i+1] == '\n': // line continuation
			i++
		case r == '\\' && i+1 < len(rs):
			i++
			word.WriteRune(rs[i])
			inWord = true
		case r == '`' || (r == '$' && i+1 < len(rs) && rs[i+1] == '('):
			unsafe = true
			word.WriteRune(r)
			inWord = true
		case r == ';' || r == '\n' || r == '|' || r == '&':
			flushCmd()
			for i+1 < len(rs) && (rs[i+1] == '|' || rs[i+1] == '&') {
				i++
			}
		case r == ' ' || r == '\t' || r == '\r':
			flushWord()
		case r == '#' && !inWord:
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			flushCmd()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	flushCmd()
	return cmds, unsafe
}

// Commands returns every simple command a shell line would run, expanding
// `bash -c "..."` (and eval) up to a bounded depth, with wrappers and variable
// assignments removed. unsafe reports constructs that cannot be inspected
// statically ($(...) or backticks); callers should treat them as suspicious.
func Commands(line string) (out []Command, unsafe bool) {
	return commands(line, 0)
}

func commands(line string, depth int) ([]Command, bool) {
	cmds, unsafe := splitWords(line)
	var out []Command
	for _, c := range cmds {
		c = stripPrefix(c)
		if len(c) == 0 {
			continue
		}
		exe := baseName(c[0])
		if (shells[exe] || exe == "eval") && depth < maxShellDepth {
			if i := flagIndex(c, "-c"); i >= 0 && i+1 < len(c) {
				inner, u := commands(c[i+1], depth+1)
				out, unsafe = append(out, inner...), unsafe || u
				continue
			}
			if exe == "eval" && len(c) > 1 {
				inner, u := commands(strings.Join(c[1:], " "), depth+1)
				out, unsafe = append(out, inner...), unsafe || u
				continue
			}
		}
		out = append(out, c)
	}
	return out, unsafe
}

func flagIndex(c Command, flag string) int {
	for i, w := range c {
		if w == flag || (strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.Contains(w, "c") && len(w) <= 4 && strings.HasSuffix(w, "c")) {
			return i
		}
	}
	return -1
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func isAssignment(w string) bool {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return false
	}
	for j, r := range w[:i] {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || j > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// stripPrefix drops VAR=val assignments and wrapper commands (with their
// flags) from the front of a command.
func stripPrefix(c Command) Command {
	for len(c) > 0 {
		switch {
		case isAssignment(c[0]):
			c = c[1:]
		case wrappers[baseName(c[0])]:
			wname := baseName(c[0])
			c = c[1:]
			for len(c) > 0 && (strings.HasPrefix(c[0], "-") || isAssignment(c[0]) || (wname == "timeout" && len(c) > 1 && isDuration(c[0]))) {
				c = c[1:]
			}
		default:
			return c
		}
	}
	return c
}

func isDuration(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r == '.' || r == 's' || r == 'm' || r == 'h' || r == 'd') {
			return false
		}
	}
	return true
}

// Executable returns the command's program name.
func (c Command) Executable() string {
	if len(c) == 0 {
		return ""
	}
	return baseName(c[0])
}

// Args returns the words after the program.
func (c Command) Args() []string {
	if len(c) == 0 {
		return nil
	}
	return c[1:]
}
