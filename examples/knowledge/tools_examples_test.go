package knowledge_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestToolsExamples runs the offline agent and MCP examples and checks that
// the tool they expose shows up in the output.
func TestToolsExamples(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "agent_tools", args: []string{"run", "./agent_tools"}, want: "rag_answer"},
		{name: "mcp_server -demo", args: []string{"run", "./mcp_server", "-demo"}, want: "knowledge_answer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "go", tc.args...).CombinedOutput()
			if err != nil {
				t.Fatalf("go %s: %v\n%s", strings.Join(tc.args, " "), err, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}
