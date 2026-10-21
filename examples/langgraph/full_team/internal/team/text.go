package team

import (
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
	return messages.AI(Brain(m.Text()))
}

func head(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
