package team

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// field returns the rest of the line that starts with key.
func field(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), key); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// after returns everything after key to the end.
func after(text, key string) string {
	if _, v, ok := strings.Cut(text, key); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// offline answers by the ROLE line of the last human message, in the format
// each skill defines. It is the scripted stand-in for every model.
func offline(msgs []messages.Message) messages.Message {
	m, _ := messages.LastOfRole(msgs, messages.RoleHuman)
	if field(m.Text(), "ROLE:") == "po" {
		return offlinePO(msgs, m.Text())
	}
	return messages.AI(Brain(m.Text()))
}

// backendNamed reads the ROOT: and URL: lines of the PO prompt's backend block.
func backendNamed(prompt string) (roots, urls []string) {
	block, _, _ := strings.Cut(after(prompt, "## Backend named by the demand"), "\n## ")
	for _, l := range strings.Split(block, "\n") {
		l = strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(l, "ROOT:"); ok {
			roots = append(roots, strings.TrimSpace(v))
		} else if v, ok := strings.CutPrefix(l, "URL:"); ok {
			urls = append(urls, strings.TrimSpace(v))
		}
	}
	return roots, urls
}

// offlinePO is the scripted PO. When the demand names a backend it first calls
// backend_read, backend_grep and backend_get on it in one turn, then answers
// with the spec plus an EVIDENCE block citing what the tools returned and the
// OPEN ITEMS they left. With no backend named it calls nothing.
func offlinePO(msgs []messages.Message, prompt string) messages.Message {
	start := 0
	for i, m := range msgs {
		if m.Role == messages.RoleHuman {
			start = i
		}
	}
	args := map[string]string{} // call id -> what it read
	var evidence, problems []string
	for _, m := range msgs[start:] {
		for _, tc := range m.ToolCalls {
			if strings.HasPrefix(tc.Name, "backend_") {
				args[tc.ID] = fmt.Sprint(tc.Args["path"], tc.Args["url"])
			}
		}
		if m.Role == messages.RoleTool && strings.HasPrefix(m.Name, "backend_") {
			line := fmt.Sprintf("- %s %s: %s", m.Name, strings.ReplaceAll(args[m.ToolCallID], "<nil>", ""), head(m.Text(), 200))
			if m.Status == "error" {
				problems = append(problems, line)
			} else {
				evidence = append(evidence, line)
			}
		}
	}
	roots, urls := backendNamed(prompt)
	if len(args) == 0 && len(roots)+len(urls) > 0 {
		var calls []messages.ToolCall
		add := func(name string, a map[string]any) {
			calls = append(calls, messages.ToolCall{ID: fmt.Sprintf("po-%s-%d-%d", name, len(msgs), len(calls)), Name: name, Args: a})
		}
		for _, r := range roots {
			add("backend_read", map[string]any{"path": filepath.Join(r, "openapi.yaml")})
			add("backend_grep", map[string]any{"pattern": "coupon", "path": r})
		}
		for _, u := range urls {
			add("backend_get", map[string]any{"url": strings.TrimRight(u, "/") + "/coupons/SAVE10"})
		}
		return messages.AI("", calls...)
	}
	out := Brain(prompt)
	if len(evidence) > 0 {
		out += "\nEVIDENCE:\n" + strings.Join(evidence, "\n")
	}
	if len(args) > 0 {
		if len(problems) == 0 {
			problems = []string{"- none"}
		}
		out += "\nOPEN ITEMS:\n" + strings.Join(problems, "\n")
	}
	return messages.AI(out)
}

// OfflineWithTool answers like offline, but first calls the named tool with no
// arguments on each new turn, so its result reaches the model and the
// compression middleware sees a tool message. The call id grows with the
// thread, so it stays unique.
func OfflineWithTool(name string) func([]messages.Message) messages.Message {
	return func(msgs []messages.Message) messages.Message {
		if last, ok := messages.Last(msgs); ok && last.Role == messages.RoleHuman {
			return messages.AI("", messages.ToolCall{ID: fmt.Sprintf("%s-%d", name, len(msgs)), Name: name, Args: map[string]any{}})
		}
		return offline(msgs)
	}
}

func head(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
