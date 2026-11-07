package team

import (
	"testing"

	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
	"github.com/fsvxavier/golang-chain-eco/providers/claudecli"
)

func TestAttachMCP(t *testing.T) {
	t.Setenv("AGENT_MCP_TOKENS", t.TempDir())
	t.Setenv("AGENT_MCP", "docs=https://a.example/mcp, wiki=https://b.example/mcp")
	a := anthropic.New("claude-x")
	if err := attachMCP(a); err != nil || len(a.MCPServers) != 2 || a.MCPServers[0].Name != "docs" || a.MCPServers[0].OAuth == nil {
		t.Fatalf("%v %+v", err, a.MCPServers)
	}
	c := claudecli.New("sonnet")
	if err := attachMCP(c); err != nil || c.MCPServers["wiki"] != "https://b.example/mcp" {
		t.Fatalf("%v %+v", err, c.MCPServers)
	}
	t.Setenv("AGENT_MCP", "broken")
	if err := attachMCP(anthropic.New("claude-x")); err == nil {
		t.Fatal("want a parse error")
	}
}
