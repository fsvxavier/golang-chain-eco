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
	n := len(Cast)
	if want := fmt.Sprintf("%d of %d present", n, n); !strings.Contains(text, want) {
		t.Fatalf("want %q in:\n%s", want, text)
	}
	for _, w := range Cast {
		var row string
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "- "+w.ID+" ") {
				row = l
			}
		}
		if row == "" {
			t.Fatalf("no row for %s in:\n%s", w.ID, text)
		}
		for _, want := range []string{"(" + w.Role + ")", "model offline", "present: Presente! " + w.ID} {
			if !strings.Contains(row, want) {
				t.Errorf("row %q lacks %q", row, want)
			}
		}
	}
}

func TestPresentAbsentWhenWorkerDown(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Hub: NewHubClient(l.HubURL), HubURL: l.HubURL, PresentTimeout: 2 * time.Second}
	l.Workers["qa-2"]()
	n := len(Cast)
	want := fmt.Sprintf("%d of %d present", n-1, n)
	var text string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		out, err := presentTool(t, b).Call(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		if text = fmt.Sprint(out); strings.Contains(text, want) {
			break
		}
	}
	if !strings.Contains(text, want) || !strings.Contains(text, "Absent: qa-2") || !strings.Contains(text, "- qa-2 (qa) [model offline] absent") {
		t.Fatalf("want %q and qa-2 absent in:\n%s", want, text)
	}
}

func TestPresentPromptListed(t *testing.T) {
	b := &Bridge{Hub: NewHubClient("http://127.0.0.1:1"), HubURL: "http://127.0.0.1:1"}
	s := b.MCPServer(ctx(t))
	call := func(method, params string) string {
		out := s.Handle(ctx(t), []byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		return string(out)
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
