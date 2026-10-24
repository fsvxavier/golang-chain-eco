// Package qa gives the QA role an optional browser_test tool: it runs the
// operator's Playwright command against the app URL the demand named. Without
// that command or URL the tool says NOT RUN and QA reviews the tests on paper.
package qa

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

// MaxSpec caps the spec argument (bytes).
const MaxSpec = 256 << 10

// Runner runs a test spec against appURL and returns the run's output.
type Runner func(ctx context.Context, appURL, spec string) (string, error)

type args struct {
	Spec string `json:"spec" jsonschema:"the Playwright test source to run against the app"`
}

func notRun(why string) (any, error) { return "NOT RUN: " + why, nil }

// Tool is browser_test. The app URL comes only from the scope, never from the
// model; a missing command, scope or URL is a normal "NOT RUN" result.
func Tool(scope func(ctx context.Context) (team.Scope, error), run Runner) tools.Tool {
	return tools.New("browser_test", "Run Playwright tests against the front-end app the demand named. Returns the run output, or NOT RUN with the reason when no runner or app URL is configured.",
		func(ctx context.Context, a args) (any, error) {
			if len(a.Spec) > MaxSpec {
				return nil, tools.Errorf("browser_test: spec over %d bytes", MaxSpec)
			}
			if run == nil {
				return notRun("no PLAYWRIGHT_CMD is configured")
			}
			s, err := scope(ctx)
			if err != nil || s.AppURL == "" {
				return notRun("the demand named no app_url")
			}
			if u, err := url.Parse(s.AppURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
				return notRun("app_url is not an http(s) URL with a host")
			}
			out, err := run(ctx, s.AppURL, a.Spec)
			if err != nil {
				return "FAILED: " + err.Error() + "\n" + out, nil // a failing run is a result QA must read
			}
			return out, nil
		})
}

// RoleTools is a team.LocalOptions.RoleTools giving only the qa role the tool.
// hub may return nil until the team is up, which means no scope.
func RoleTools(hub func() *team.HubClient, cmd string) func(role string) []tools.Tool {
	t := Tool(func(ctx context.Context) (team.Scope, error) { return team.ScopeFromContext(ctx, hub()) }, CommandRunner(cmd))
	return func(role string) []tools.Tool {
		if role == "qa" {
			return []tools.Tool{t}
		}
		return nil
	}
}

// CommandRunner runs cmd (split with strings.Fields: no shell, nothing from the
// model on the command line). The spec goes to a file in a fresh 0700 temp dir,
// the child's working directory, and only APP_URL and SPEC_FILE reach it.
// An empty cmd returns a nil Runner.
func CommandRunner(cmd string) Runner { return commandRunner(cmd, 1<<20, 2*time.Minute) }

func commandRunner(cmd string, maxOut int, timeout time.Duration) Runner {
	argv := strings.Fields(cmd)
	if len(argv) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return func(ctx context.Context, appURL, spec string) (string, error) {
		dir, err := os.MkdirTemp("", "qa-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		file := filepath.Join(dir, "spec.test.js")
		if err := os.WriteFile(file, []byte(spec), 0o600); err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Dir = dir
		c.Env = append(os.Environ(), "APP_URL="+appURL, "SPEC_FILE="+file)
		out := &capped{max: maxOut}
		c.Stdout, c.Stderr = out, out
		err = c.Run()
		if ctx.Err() != nil {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		return out.String(), err
	}
}

// capped keeps the first max bytes and notes what it dropped.
type capped struct {
	buf  bytes.Buffer // not embedded: its ReadFrom would bypass the cap
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.over = true
	} else {
		c.buf.Write(p)
	}
	return len(p), nil
}

func (c *capped) String() string {
	if c.over {
		return c.buf.String() + "\n[output truncated]"
	}
	return c.buf.String()
}
