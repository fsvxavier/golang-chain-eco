package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// writeFiles creates name -> content files under a fresh temp dir and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// kagScript plays the model offline: extraction returns the Dr. Silva triple
// for chunks that mention Silva, the planner starts from Dr. Silva, and the
// synthesis (no tools) answers with a cited sentence.
func kagScript() *models.Fake {
	return &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		if len(o.Tools) == 0 {
			return messages.AI("Dr. Silva prescreveu Varfarina. [1]"), nil
		}
		var args map[string]any
		switch o.Tools[0].Name {
		case "extraction":
			args = map[string]any{"entities": []any{}, "relations": []any{}}
			if last, _ := messages.Last(msgs); strings.Contains(last.Text(), "Silva") {
				args = map[string]any{
					"entities": []any{
						map[string]any{"name": "Dr. Silva", "type": "Medico"},
						map[string]any{"name": "Varfarina", "type": "Droga"},
					},
					"relations": []any{
						map[string]any{"from": "Dr. Silva", "type": "PRESCREVEU", "to": "Varfarina"},
					},
				}
			}
		case "Plan":
			args = map[string]any{"steps": []any{
				map[string]any{"id": "s1", "goal": "o que o médico prescreveu", "entity": "Dr. Silva",
					"relations": []any{"PRESCREVEU"}, "dir": "out"},
			}}
		}
		return messages.AI("", messages.ToolCall{ID: "t1", Name: o.Tools[0].Name, Args: args}), nil
	}}
}

func TestIndexThenAsk(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.md": "Go channels pass messages between goroutines.",
		"b.md": "Feijoada is a traditional Brazilian stew.",
	})
	out := t.TempDir()
	d := deps{chat: offlineModel()}
	var buf bytes.Buffer
	if err := runWith([]string{"index", dir, "--out", out}, &buf, io.Discard, d); err != nil {
		t.Fatalf("index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "vectors.json")); err != nil {
		t.Fatalf("vectors.json not written: %v", err)
	}

	buf.Reset()
	if err := runWith([]string{"ask", "How do channels work in Go?", "--mode", "rag", "--dir", out}, &buf, io.Discard, d); err != nil {
		t.Fatalf("ask: %v", err)
	}
	firstSource := ""
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "  [1] ") {
			firstSource = line
		}
	}
	if !strings.Contains(firstSource, "a.md") {
		t.Fatalf("top source should be a.md, got %q\n%s", firstSource, buf.String())
	}
	if !strings.Contains(buf.String(), "trace:") || !strings.Contains(buf.String(), "route: rag") {
		t.Fatalf("missing trace:\n%s", buf.String())
	}
}

func TestIndexKagThenAsk(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"receita.md": "Dr. Silva prescreveu Varfarina ao paciente João.",
		"bula.md":    "Varfarina interage com Aspirina.",
	})
	out := t.TempDir()
	d := deps{chat: kagScript()}
	if err := runWith([]string{"index", dir, "--kind", "both", "--out", out}, io.Discard, io.Discard, d); err != nil {
		t.Fatalf("index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.json")); err != nil {
		t.Fatalf("graph.json not written: %v", err)
	}

	var buf bytes.Buffer
	if err := runWith([]string{"ask", "Qual remédio foi prescrito pelo Dr. Silva?", "--mode", "kag", "--dir", out}, &buf, io.Discard, d); err != nil {
		t.Fatalf("ask: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Dr. Silva prescreveu Varfarina") {
		t.Errorf("answer missing:\n%s", got)
	}
	if !strings.Contains(got, "receita.md") {
		t.Errorf("provenance (receita.md) missing from sources:\n%s", got)
	}
	if !strings.Contains(got, "step:s1") {
		t.Errorf("trace missing step:s1:\n%s", got)
	}
}

func TestAskKagNeedsGraph(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.md": "Go channels."})
	out := t.TempDir()
	d := deps{chat: offlineModel()}
	if err := runWith([]string{"index", dir, "--out", out}, io.Discard, io.Discard, d); err != nil {
		t.Fatal(err)
	}
	err := runWith([]string{"ask", "quem?", "--mode", "kag", "--dir", out}, io.Discard, io.Discard, d)
	if err == nil || !strings.Contains(err.Error(), "graph.json") {
		t.Fatalf("want error mentioning graph.json, got %v", err)
	}
}

func TestAskCagNeedsSrc(t *testing.T) {
	err := runWith([]string{"ask", "quem?", "--mode", "cag", "--dir", t.TempDir()}, io.Discard, io.Discard, deps{chat: offlineModel()})
	if err == nil || !strings.Contains(err.Error(), "--src") {
		t.Fatalf("want error mentioning --src, got %v", err)
	}
}

func TestUsageOnNoArgs(t *testing.T) {
	var buf bytes.Buffer
	if err := runWith(nil, &buf, io.Discard, deps{}); err != nil {
		t.Fatalf("no args: %v", err)
	}
	if !strings.Contains(buf.String(), "knowledge index") || !strings.Contains(buf.String(), "knowledge ask") {
		t.Fatalf("usage not printed:\n%s", buf.String())
	}
}

func TestUnknownMode(t *testing.T) {
	err := runWith([]string{"ask", "quem?", "--mode", "magic"}, io.Discard, io.Discard, deps{chat: offlineModel()})
	if err == nil || !strings.Contains(err.Error(), "unknown --mode") {
		t.Fatalf("want unknown --mode error, got %v", err)
	}
}
