package agentprotocol

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is a server-to-client message of the streaming protocol.
type Event struct {
	Type    string         `json:"type"` // always "event"
	EventID string         `json:"eventId,omitempty"`
	Seq     uint64         `json:"seq"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// channelOf maps an event method to its subscription channel: most events use
// the channel name; the input family uses "input.requested".
func channelOf(method string) string {
	if strings.HasPrefix(method, "input.") {
		return "input"
	}
	return method
}

// Filter selects events for one subscription (EventStreamRequest).
type Filter struct {
	Channels   []string   `json:"channels"`
	Namespaces [][]string `json:"namespaces,omitempty"`
	Depth      *int       `json:"depth,omitempty"`
	Since      uint64     `json:"since,omitempty"`
}

func (f Filter) wantsChannel(ch string) bool {
	for _, c := range f.Channels {
		if c == ch || (strings.HasPrefix(c, "custom:") && ch == "custom") {
			return true
		}
	}
	return false
}

func (f Filter) wantsNamespace(ns []string) bool {
	if len(f.Namespaces) == 0 {
		return f.Depth == nil || len(ns) <= *f.Depth
	}
	for _, prefix := range f.Namespaces {
		if len(ns) < len(prefix) || !slices.Equal(ns[:len(prefix)], prefix) {
			continue
		}
		if f.Depth == nil || len(ns)-len(prefix) <= *f.Depth {
			return true
		}
	}
	return false
}

// Match reports whether e passes the filter.
func (f Filter) Match(e Event) bool {
	ns, _ := e.Params["namespace"].([]string)
	return f.wantsChannel(channelOf(e.Method)) && f.wantsNamespace(ns)
}

// hub fans a thread's events out to subscribers and keeps a ring buffer so
// late or reconnecting clients can replay by sequence number.
type hub struct {
	mu   sync.Mutex
	seq  uint64
	buf  []Event
	cap  int
	subs map[*subscription]struct{}
}

type subscription struct {
	f  Filter
	ch chan Event
	// dropped is set when the subscriber could not keep up and was cut off.
	dropped bool
}

func newHub(capacity int) *hub {
	return &hub{cap: capacity, subs: map[*subscription]struct{}{}}
}

// publish assigns the next sequence number and delivers to subscribers.
func (h *hub) publish(method string, ns []string, node string, data any) Event {
	params := map[string]any{"namespace": append([]string{}, ns...), "timestamp": time.Now().UnixMilli(), "data": data}
	if node != "" {
		params["node"] = node
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e := Event{Type: "event", EventID: "evt_" + strconv.FormatUint(h.seq, 10), Seq: h.seq, Method: method, Params: params}
	h.buf = append(h.buf, e)
	if len(h.buf) > h.cap {
		h.buf = h.buf[len(h.buf)-h.cap:]
	}
	for s := range h.subs {
		if !s.f.Match(e) {
			continue
		}
		select {
		case s.ch <- e:
		default: // slow consumer: cut it off rather than block the run
			s.dropped = true
			close(s.ch)
			delete(h.subs, s)
		}
	}
	return e
}

// lastSeq returns the newest sequence number.
func (h *hub) lastSeq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// subscribe returns the replayed backlog (events after f.Since) and a live
// channel, atomically so nothing is missed or duplicated.
func (h *hub) subscribe(f Filter) ([]Event, *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var backlog []Event
	if f.Since > 0 {
		for _, e := range h.buf {
			if e.Seq > f.Since && f.Match(e) {
				backlog = append(backlog, e)
			}
		}
	}
	s := &subscription{f: f, ch: make(chan Event, 1024)}
	h.subs[s] = struct{}{}
	return backlog, s
}

func (h *hub) unsubscribe(s *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.ch)
	}
}
