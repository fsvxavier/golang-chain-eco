package team

import (
	"fmt"
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
	if field(m.Text(), "ROLE:") == "po" && strings.Contains(m.Text(), "MODE: rework") {
		return messages.AI("DECISION: FIX\nDIRECTION: address the feedback below and resubmit.")
	}
	return messages.AI(Brain(m.Text()))
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
