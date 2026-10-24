package team

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

func presentTool(t *testing.T, b *Bridge) tools.Tool {
	t.Helper()
	for _, tl := range b.Tools() {
		if tl.Name() == "team_present" {
			return tl
		}
	}
	t.Fatal("no tool team_present")
	return nil
}

func rowOf(text, id string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "- "+id+" ") {
			return l
		}
	}
	return ""
}

// pollRoll calls team_present until the text contains want (or 15 s pass).
func pollRoll(t *testing.T, b *Bridge, want string) string {
	t.Helper()
	var text string
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		out, err := presentTool(t, b).Call(ctx(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if text = fmt.Sprint(out); strings.Contains(text, want) {
			break
		}
	}
	return text
}

func TestPresentToolRollCall(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Hub: NewHubClient(l.HubURL), HubURL: l.HubURL}
	out, err := presentTool(t, b).Call(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprint(out)
	if !strings.Contains(text, "12 of 12 present") {
		t.Fatalf("want 12 of 12 in:\n%s", text)
	}
	for _, w := range Cast {
		row := rowOf(text, w.ID)
		if row == "" {
			t.Fatalf("no row for %s in:\n%s", w.ID, text)
		}
		for _, want := range []string{"(" + w.Role + ")", "model offline", "present: Presente! " + w.ID} {
			if !strings.Contains(row, want) {
				t.Errorf("row %q lacks %q", row, want)
			}
		}
	}
	t.Log("\n" + text)
}

func TestPresentAbsentWhenWorkerDown(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Hub: NewHubClient(l.HubURL), HubURL: l.HubURL, PresentTimeout: 2 * time.Second}
	l.Workers["qa-2"]()
	text := pollRoll(t, b, "11 of 12 present")
	if !strings.Contains(text, "11 of 12 present") || !strings.Contains(text, "Absent: qa-2") || !strings.Contains(rowOf(text, "qa-2"), "absent") {
		t.Fatalf("want 11 of 12 and qa-2 absent in:\n%s", text)
	}
}

// A worker whose lease expired is gone from the hub, but the roll still expects it.
func TestPresentAbsentAfterLeaseExpired(t *testing.T) {
	t.Setenv("HUB_LEASE_TTL", "600ms")
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Hub: NewHubClient(l.HubURL), HubURL: l.HubURL, PresentTimeout: 2 * time.Second}
	l.Workers["qa-2"]()
	text := pollRoll(t, b, "not registered")
	t.Log("\n" + text)
	row := rowOf(text, "qa-2")
	if !strings.Contains(row, "absent: not registered (lease expired or never started)") ||
		!strings.Contains(text, "11 of 12 present") || !strings.Contains(text, "Absent: qa-2") {
		t.Fatalf("want qa-2 not registered and 11 of 12 in:\n%s", text)
	}
}

func TestPresentExtraWorkerNotCounted(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hc := NewHubClient(l.HubURL)
	if _, err := hc.Register(c, "ghost-1", "qa", "http://127.0.0.1:1", "offline"); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Hub: hc, HubURL: l.HubURL, PresentTimeout: 2 * time.Second}
	out, err := presentTool(t, b).Call(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprint(out)
	if row := rowOf(text, "ghost-1"); !strings.Contains(row, "extra") {
		t.Fatalf("ghost-1 row %q is not marked extra:\n%s", row, text)
	}
	if !strings.Contains(text, "12 of 12 present") || strings.Contains(text, "Absent: ghost-1") {
		t.Fatalf("an extra worker must not count:\n%s", text)
	}
}

func TestPresentPromptListed(t *testing.T) {
	b := &Bridge{Hub: NewHubClient("http://127.0.0.1:1"), HubURL: "http://127.0.0.1:1"}
	s := b.MCPServer(ctx(t))
	call := func(method, params string) string {
		return string(s.Handle(ctx(t), []byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`)))
	}
	if l := call("prompts/list", `{}`); !strings.Contains(l, `"present"`) || !strings.Contains(l, "Roll call: every persona answers Presente") {
		t.Fatalf("prompts/list: %s", l)
	}
	g := call("prompts/get", `{"name":"present"}`)
	for _, want := range []string{"team_present", "N of M present", "Presente"} {
		if !strings.Contains(g, want) {
			t.Errorf("prompts/get lacks %q: %s", want, g)
		}
	}
}

func TestBrainRollCall(t *testing.T) {
	got := Brain("ROLL CALL: attendance\nWORKER: senior-1 ROLE: senior\nanswer")
	if !strings.HasPrefix(got, "Presente! senior-1 (senior)") {
		t.Fatalf("got %q", got)
	}
	// Without the marker the scripted answers are unchanged.
	if got := Brain("ROLE: qa\nTASK: T1\nATTEMPT: 1"); !strings.HasPrefix(got, "VERDICT:") {
		t.Fatalf("qa answer changed: %q", got)
	}
}

func TestRegisterCarriesModel(t *testing.T) {
	hub := NewHub(time.Minute)
	ts := newTestServer(t, hub.Handler(nil))
	if _, err := NewHubClient(ts.URL).Register(ctx(t), "dev-1", "junior", "u", "anthropic:claude-haiku-5-5"); err != nil {
		t.Fatal(err)
	}
	s, err := NewHubClient(ts.URL).State(ctx(t))
	if err != nil || len(s.Workers) != 1 || s.Workers[0].Model != "anthropic:claude-haiku-5-5" {
		t.Fatalf("%+v %v", s, err)
	}
}
