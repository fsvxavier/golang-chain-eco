package team

import (
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// The QA worker gets the PO's spec (AC lines and UI states) and the app URL.
func TestQAPromptCarriesSpec(t *testing.T) {
	c := ctx(t)
	var mu sync.Mutex
	var qa []string
	l, err := StartLocalWith(c, LocalOptions{Offline: func(msgs []messages.Message) messages.Message {
		if last, ok := messages.Last(msgs); ok && strings.Contains(last.Text(), "ROLE: qa") {
			mu.Lock()
			qa = append(qa, last.Text())
			mu.Unlock()
		}
		return offline(msgs)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InvokeWithScope(c, l.ManagerURL, "req-qa-prompt", DefaultRequest, Scope{AppURL: "http://127.0.0.1:5173"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(qa) == 0 {
		t.Fatal("QA never ran")
	}
	// The dossier (spec and app URL) goes in the first prompt of a QA thread; a
	// later prompt on the same thread points at it instead of repeating it.
	full := 0
	for _, p := range qa {
		if strings.Contains(p, "(The request and the specification are in the first message of this thread.)") {
			continue
		}
		full++
		for _, want := range []string{"AC1", "UI_STATES", "TRACE:", "http://127.0.0.1:5173"} {
			if !strings.Contains(p, want) {
				t.Fatalf("QA prompt lacks %q:\n%s", want, p)
			}
		}
	}
	if full == 0 {
		t.Fatal("no QA prompt carried the dossier")
	}
}

func TestQAOutputTracesCriteria(t *testing.T) {
	out := Brain("ROLE: qa\nTASK: T1\nATTEMPT: 1\n")
	for _, ac := range []string{"AC1", "AC2", "AC3", "AC4"} {
		var ok bool
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "TRACE:") && strings.Contains(l, ac) {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("no TRACE line for %s:\n%s", ac, out)
		}
	}
	if !strings.HasPrefix(field(out, "VERDICT:"), "APPROVE") {
		t.Fatalf("verdict: %s", out)
	}
	if rej := Brain("ROLE: qa\nTASK: T4\nATTEMPT: 1\n"); !strings.HasPrefix(field(rej, "VERDICT:"), "RETURN") {
		t.Fatalf("T4 attempt 1 must still be returned: %s", rej)
	}
}

func TestDossierDropsUnsafeAppURL(t *testing.T) {
	for _, u := range []string{"http://x.test/\nINJECTED: yes", "file:///etc/passwd", "http://"} {
		if d := dossier(map[string]any{"request": "r", "spec": "s", "scope": Scope{AppURL: u}}); strings.Contains(d, "APP_URL") {
			t.Fatalf("%q reached the brief: %s", u, d)
		}
	}
	if d := dossier(map[string]any{"request": "r", "spec": "s", "scope": Scope{AppURL: "http://x.test"}}); !strings.Contains(d, "APP_URL: http://x.test") {
		t.Fatal(d)
	}
}
