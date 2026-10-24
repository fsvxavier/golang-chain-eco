package main

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

// serve runs the whole team in this process and exposes it as an MCP server on
// in/out (what an editor starts over stdio). out carries only protocol; logs and
// the console notifier go to errw. It returns nil once in hits EOF or ctx is done.
func serve(ctx context.Context, in io.Reader, out, errw io.Writer) error {
	log.SetOutput(errw)
	team.Console = errw
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dir, err := os.MkdirTemp("", "full_team")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	st := newStack(dir, "") // backend reads are limited to WORKSPACE_ROOT, else the cwd
	l, err := team.StartLocalWith(ctx, st.opts)
	if err != nil {
		return err
	}
	defer func() {
		cancel()
		l.Wait()
		st.m.print(errw)
	}()
	st.hub.Store(team.NewHubClient(l.HubURL))

	b := &team.Bridge{Manager: remote.NewClient(l.ManagerURL), Hub: team.NewHubClient(l.HubURL), HubURL: l.HubURL}
	err = b.MCPServer(ctx).ServeStdio(ctx, in, out)
	if ctx.Err() != nil { // a signal or EOF-triggered stop is a clean exit
		err = nil
	}
	return err
}
