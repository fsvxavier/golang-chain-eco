package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/mcp"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
	"github.com/fsvxavier/golang-chain-eco/providers/claudecli"
)

// RemoteMCP parses AGENT_MCP, "name=url,name2=url2": OAuth-protected MCP servers
// Claude calls itself. Empty when unset.
func RemoteMCP() (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("AGENT_MCP"), ",") {
		if kv = strings.TrimSpace(kv); kv == "" {
			continue
		}
		name, u, ok := strings.Cut(kv, "=")
		if !ok || name == "" || u == "" {
			return nil, fmt.Errorf("AGENT_MCP: %q is not name=url", kv)
		}
		out[name] = u
	}
	return out, nil
}

var (
	oauthMu sync.Mutex
	oauths  = map[string]*mcp.OAuth{} // one login per server and process, shared by every role
)

// oauthFor keeps the server's tokens in AGENT_MCP_TOKENS (default
// <user config dir>/golang-chain-eco/mcp), so one login serves every process.
func oauthFor(name string) *mcp.OAuth {
	oauthMu.Lock()
	defer oauthMu.Unlock()
	if o, ok := oauths[name]; ok {
		return o
	}
	dir := os.Getenv("AGENT_MCP_TOKENS")
	if dir == "" {
		cfg, _ := os.UserConfigDir()
		dir = filepath.Join(cfg, "golang-chain-eco", "mcp")
	}
	_ = os.MkdirAll(dir, 0o700)
	o := &mcp.OAuth{TokenFile: filepath.Join(dir, name+".json"),
		// Not signed in: show the authorization link and open it in the browser.
		Authorize: func(_ context.Context, u string) error {
			fmt.Fprintf(os.Stderr, "mcp %s: sign in at (opening the browser):\n%s\n", name, u)
			openBrowser(u)
			return nil
		}}
	oauths[name] = o
	return o
}

// attachMCP gives the model the AGENT_MCP servers. With the Anthropic API the
// provider's MCP connector sends the OAuth token this process holds; with the
// claude CLI (the subscription) the CLI does the OAuth with its own login.
func attachMCP(m models.ChatModel) error {
	servers, err := RemoteMCP()
	if err != nil || len(servers) == 0 {
		return err
	}
	switch c := m.(type) {
	case *anthropic.Chat:
		for _, name := range sortedKeys(servers) {
			c.MCPServers = append(c.MCPServers, anthropic.MCPServer{Name: name, URL: servers[name], OAuth: oauthFor(name)})
		}
	case *claudecli.Chat:
		c.MCPServers = servers
	}
	return nil
}

// openBrowser is best effort: the link is printed too.
func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		for _, bin := range []string{"wslview", "xdg-open"} { // wslview opens the Windows browser from WSL
			if p, err := exec.LookPath(bin); err == nil {
				cmd = exec.Command(p, u)
				break
			}
		}
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

// MCPLogin signs in to every AGENT_MCP server up front and saves the tokens, so
// no worker stops to ask in the middle of a job. It is optional: a worker
// without a token does the same login (one at a time per server) when it first needs it. The claude CLI backend needs no
// login here: authorize its servers with `claude mcp`.
func MCPLogin(ctx context.Context) error {
	servers, err := RemoteMCP()
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return fmt.Errorf("AGENT_MCP is empty (name=url,...)")
	}
	for _, name := range sortedKeys(servers) {
		if _, err := oauthFor(name).Access(ctx, servers[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Printf("%s: signed in\n", name)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
