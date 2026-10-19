package knowledge_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runExample runs one offline example with `go run` and returns its output.
func runExample(t *testing.T, dir string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "run", "./"+dir).CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./%s: %v\n%s", dir, err, out)
	}
	return string(out)
}

func TestKagRouterExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go run")
	}

	kagOut := runExample(t, "kag")
	for _, want := range []string{"PRESCREVEU", "INTERAGE_COM"} {
		if !strings.Contains(kagOut, want) {
			t.Errorf("kag output missing %q:\n%s", want, kagOut)
		}
	}

	routerOut := runExample(t, "router")
	for _, want := range []string{"route: kag", "route: cag", "route: rag", "fallback"} {
		if !strings.Contains(routerOut, want) {
			t.Errorf("router output missing %q:\n%s", want, routerOut)
		}
	}
}
