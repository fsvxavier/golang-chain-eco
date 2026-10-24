package team

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Cast is the default team: id -> role. The ids follow the compose file.
var Cast = []struct{ ID, Role string }{
	{"po", "po"}, {"leader", "leader"},
	{"junior-1", "junior"}, {"junior-2", "junior"}, {"mid-1", "mid"}, {"mid-2", "mid"},
	{"senior-1", "senior"}, {"senior-2", "senior"},
	{"specialist-1", "specialist"}, {"specialist-2", "specialist"}, {"qa-1", "qa"}, {"qa-2", "qa"},
}

// Local is every service of the team running in this process, each on its own
// loopback port and talking over real HTTP, as they would across containers.
// Stop one worker's context to simulate it crashing.
type Local struct {
	HubURL, ManagerURL string
	Workers            map[string]context.CancelFunc
	wg                 sync.WaitGroup
}

func listen() (net.Listener, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	return ln, "http://" + ln.Addr().String(), err
}

// LocalOptions extends every worker of a local team.
type LocalOptions struct {
	Middleware []agents.Middleware
	Tools      []tools.Tool
	// RoleTools adds tools to the workers of one role only, after Tools (the PO's
	// backend tools). Nil, or nil for a role, adds nothing.
	RoleTools func(role string) []tools.Tool
	Offline   func([]messages.Message) messages.Message
	// CallCounter counts the backend calls of a request id for the manager's
	// evidence rule (see RunManager). Nil skips the call-count half of the rule.
	CallCounter func(requestID string) int
	// ManagerAPIKey, when set, is the x-api-key the manager requires (and wins
	// over MANAGER_API_KEY). Empty keeps the environment's value.
	ManagerAPIKey string
}

// StartLocal brings up hub, manager and the 12 workers. It returns when the hub
// answers and every worker has registered.
func StartLocal(ctx context.Context) (*Local, error) {
	return StartLocalWith(ctx, LocalOptions{})
}

// StartLocalWith is StartLocal with middleware and tools given to every worker.
// A failure (or ctx ending) while starting stops what already runs and waits
// for it before returning.
func StartLocalWith(ctx context.Context, opts LocalOptions) (_ *Local, err error) {
	ctx, cancel := context.WithCancel(ctx)
	l := &Local{Workers: map[string]context.CancelFunc{}}
	defer func() {
		if err != nil {
			cancel()
			l.wg.Wait()
		}
	}()
	hubLn, hubURL, err := listen()
	if err != nil {
		return nil, err
	}
	mgrLn, mgrURL, err := listen()
	if err != nil {
		hubLn.Close()
		return nil, err
	}
	l.HubURL, l.ManagerURL = hubURL, mgrURL
	key := opts.ManagerAPIKey
	if key == "" {
		key = os.Getenv("MANAGER_API_KEY")
	}
	l.wg.Add(2)
	go func() { defer l.wg.Done(); _ = RunHub(ctx, hubLn) }()
	go func() { defer l.wg.Done(); _ = RunManager(ctx, hubURL, mgrLn, opts.CallCounter, key) }()
	for _, c := range Cast {
		ln, _, err := listen()
		if err != nil {
			return nil, err
		}
		ts := opts.Tools
		if opts.RoleTools != nil {
			ts = append(append([]tools.Tool(nil), opts.Tools...), opts.RoleTools(c.Role)...)
		}
		wctx, wcancel := context.WithCancel(ctx)
		l.Workers[c.ID] = wcancel
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			_ = RunWorker(wctx, WorkerConfig{ID: c.ID, Role: c.Role, Hub: hubURL,
				Middleware: opts.Middleware, Tools: ts, Offline: opts.Offline}, ln)
		}()
	}
	hc := NewHubClient(hubURL)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s, err := hc.State(ctx); err == nil && len(s.Workers) == len(Cast) {
			return l, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("team did not come up")
}

// Wait blocks until every service has stopped (cancel the context first).
func (l *Local) Wait() { l.wg.Wait() }
