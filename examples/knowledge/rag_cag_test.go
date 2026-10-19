package knowledge_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestRagCagExamples runs the offline rag and cag examples and checks the
// trace markers they are meant to show.
func TestRagCagExamples(t *testing.T) {
	tests := []struct {
		dir  string
		want []string
	}{
		{dir: "./rag", want: []string{"Skipped"}},
		{dir: "./cag", want: []string{"cache_hit", "fallback"}},
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "go", "run", tc.dir).CombinedOutput()
			if err != nil {
				t.Fatalf("go run %s: %v\n%s", tc.dir, err, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(out), w) {
					t.Errorf("output of %s lacks %q:\n%s", tc.dir, w, out)
				}
			}
		})
	}
}
