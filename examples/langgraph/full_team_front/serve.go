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
	oldLog, oldConsole := log.Writer(), team.Console
	log.SetOutput(errw)
	team.Console = errw
	defer func() { log.SetOutput(oldLog); team.Console = oldConsole }()
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
	err = b.MCPServer(ctx).ServeStdio(ctx, newCtxReader(ctx, in), out)
	if ctx.Err() != nil { // cancelled by the caller (a signal): a clean stop
		err = nil
	}
	return err // on stdin EOF ServeStdio itself returns nil
}

// ctxReader makes a blocking Read give up when ctx ends: ServeStdio only
// checks its context between lines, so an idle stdin would keep serve alive
// after a signal. The pump goroutine stays blocked in the underlying Read
// until it returns or the process exits (shortcut: one leaked goroutine).
type ctxReader struct {
	ctx  context.Context
	ch   chan chunk
	rest []byte
}

type chunk struct {
	b   []byte
	err error
}

func newCtxReader(ctx context.Context, r io.Reader) *ctxReader {
	c := &ctxReader{ctx: ctx, ch: make(chan chunk)}
	go func() {
		for {
			buf := make([]byte, 32<<10)
			n, err := r.Read(buf)
			select {
			case c.ch <- chunk{buf[:n], err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return c
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if len(c.rest) == 0 {
		select {
		case ck := <-c.ch:
			if len(ck.b) == 0 {
				return 0, ck.err
			}
			c.rest = ck.b
			if ck.err != nil { // data and error together: deliver data first, error next call
				go func() { c.ch <- chunk{err: ck.err} }()
			}
		case <-c.ctx.Done():
			return 0, io.EOF
		}
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}
