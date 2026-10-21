package team

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Cast is the default team: id -> role. The ids follow the compose file.
var Cast = []struct{ ID, Role string }{
	{"po", "po"}, {"architect", "architect"}, {"leader", "leader"},
	{"junior-1", "junior"}, {"junior-2", "junior"}, {"mid-1", "mid"}, {"senior-1", "senior"},
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
	Offline    func([]messages.Message) messages.Message
}

// StartLocal brings up hub, manager and the 11 workers. It returns when the hub
// answers; workers register in the background.
func StartLocal(ctx context.Context) (*Local, error) {
	return StartLocalWith(ctx, LocalOptions{})
}

// StartLocalWith is StartLocal with middleware and tools given to every worker.
func StartLocalWith(ctx context.Context, opts LocalOptions) (*Local, error) {
	l := &Local{Workers: map[string]context.CancelFunc{}}
	hubLn, hubURL, err := listen()
	if err != nil {
		return nil, err
	}
	mgrLn, mgrURL, err := listen()
	if err != nil {
		return nil, err
	}
	l.HubURL, l.ManagerURL = hubURL, mgrURL
	l.wg.Add(2)
	go func() { defer l.wg.Done(); _ = RunHub(ctx, hubLn) }()
	go func() { defer l.wg.Done(); _ = RunManager(ctx, hubURL, mgrLn) }()
	for _, c := range Cast {
		ln, _, err := listen()
		if err != nil {
			return nil, err
		}
		wctx, cancel := context.WithCancel(ctx)
		l.Workers[c.ID] = cancel
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			_ = RunWorker(wctx, WorkerConfig{ID: c.ID, Role: c.Role, Hub: hubURL,
				Middleware: opts.Middleware, Tools: opts.Tools, Offline: opts.Offline}, ln)
		}()
	}
	hc := NewHubClient(hubURL)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := hc.State(ctx); err == nil && len(s.Workers) == len(Cast) {
			return l, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("team did not come up")
}

// Wait blocks until every service has stopped (cancel the context first).
func (l *Local) Wait() { l.wg.Wait() }
