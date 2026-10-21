package compress

import (
	"strings"
	"testing"
)

func TestPonytailDirectiveLevels(t *testing.T) {
	lite := PonytailDirective(Lite)
	full := PonytailDirective(Full)
	ultra := PonytailDirective(Ultra)

	if !(len(lite) < len(full) && len(full) < len(ultra)) {
		t.Fatalf("sizes lite=%d full=%d ultra=%d, want strictly increasing", len(lite), len(full), len(ultra))
	}
	for name, text := range map[string]string{"lite": lite, "full": full, "ultra": ultra} {
		if len(text) >= 1200 {
			t.Errorf("%s is %d bytes, want < 1200", name, len(text))
		}
		for _, word := range []string{"validation", "security"} {
			if !strings.Contains(text, word) {
				t.Errorf("%s lacks %q", name, word)
			}
		}
		lines := strings.Split(text, "\n")
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "Ponytail:") {
			t.Errorf("%s last line %q does not start with Ponytail:", name, last)
		}
	}
	if !strings.Contains(full, "Lazy about the solution, never about reading.") {
		t.Error("full lacks the reading sentence")
	}
	if !strings.Contains(full, "standard library") || !strings.Contains(full, "minimum code") {
		t.Error("full lacks the ladder")
	}
	if !strings.Contains(ultra, "ask whether it must exist") {
		t.Error("ultra lacks the existence question")
	}
	if n := strings.Count(lite, ". ") + 1; n > 2 {
		t.Errorf("lite has %d sentences, want 2", n)
	}
}

func TestPonytailDirectiveUnknownLevelIsFull(t *testing.T) {
	if PonytailDirective("bogus") != PonytailDirective(Full) {
		t.Fatal("unknown level should use the full directive")
	}
	if PonytailDirective("") != PonytailDirective(Full) {
		t.Fatal("empty level should use the full directive")
	}
}

func TestPonytailDirectiveNeverSkipsChecks(t *testing.T) {
	for _, lvl := range []Level{Lite, Full, Ultra} {
		text := strings.ToLower(PonytailDirective(lvl))
		for _, bad := range []string{"skip validation", "skip error", "skip security", "skip accessibility", "skip tests", "fewer tests"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s tells the model to %q", lvl, bad)
			}
		}
	}
}
