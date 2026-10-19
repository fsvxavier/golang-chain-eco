package agentprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sseWriter{w, f}, true
}

func (s *sseWriter) event(id, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if id != "" {
		fmt.Fprintf(s.w, "id: %s\n", id)
	}
	if event != "" {
		fmt.Fprintf(s.w, "event: %s\n", event)
	}
	_, err = fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.f.Flush()
	return err
}

func (s *sseWriter) comment(text string) {
	fmt.Fprintf(s.w, ": %s\n\n", text)
	s.f.Flush()
}

// ------------------------------------------------------- run streams (REST)

func wantMode(modes []string, name string) bool {
	if name == "metadata" || name == "error" || name == "end" {
		return true
	}
	if len(modes) == 0 {
		return name == "values"
	}
	return slices.Contains(modes, name)
}

// pumpRun streams a run's recorded events, live, until it finishes.
func (s *Server) pumpRun(ctx context.Context, sw *sseWriter, rec *runRec, modes []string) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			rec.cond.Broadcast()
		case <-stop:
		}
	}()
	i := 0
	for {
		rec.evMu.Lock()
		for i >= len(rec.events) && !rec.finished && ctx.Err() == nil {
			rec.cond.Wait()
		}
		var batch []runEvent
		if i < len(rec.events) {
			batch = slices.Clone(rec.events[i:])
			i = len(rec.events)
		}
		done := rec.finished && i >= len(rec.events)
		rec.evMu.Unlock()
		for _, ev := range batch {
			if wantMode(modes, ev.Event) {
				if err := sw.event("", ev.Event, ev.Data); err != nil {
					return
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if done {
			_ = sw.event("", "end", nil)
			return
		}
	}
}

func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	rec := s.findRun(r.PathValue("run_id"))
	if rec == nil {
		writeErr(w, 404, "not_found", "run not found")
		return
	}
	sw, ok := newSSE(w)
	if !ok {
		writeErr(w, 500, "internal", "streaming unsupported")
		return
	}
	s.pumpRun(r.Context(), sw, rec, r.URL.Query()["stream_mode"])
}

func (s *Server) createAndStream(w http.ResponseWriter, r *http.Request) {
	body, in, err := s.parseRun(r)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	rec, err := s.startRun(r.Context(), body, in, nil, rejectIfBusy)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	sw, ok := newSSE(w)
	if !ok {
		rec.cancel()
		writeErr(w, 500, "internal", "streaming unsupported")
		return
	}
	s.pumpRun(r.Context(), sw, rec, body.StreamMode)
	if r.Context().Err() != nil && body.OnDisconnect == "cancel" {
		rec.cancel()
	}
}

// -------------------------------------------------- thread streams (events)

func (s *Server) threadFor(ctx context.Context, id string, create bool) *threadRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	th := s.lookup(ctx, id)
	if th == nil && create {
		th = s.newThread(id, nil)
	}
	return th
}

func (s *Server) threadSSE(w http.ResponseWriter, r *http.Request) {
	var f Filter
	if err := decodeBody(r, &f); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	if len(f.Channels) == 0 {
		writeErr(w, 422, "invalid_request", "channels must contain at least one channel")
		return
	}
	// streams may be opened before the first run.start: the thread is created lazily
	th := s.threadFor(r.Context(), r.PathValue("thread_id"), true)
	if lid := r.Header.Get("Last-Event-ID"); lid != "" && f.Since == 0 {
		var n uint64
		if _, err := fmt.Sscanf(lid, "evt_%d", &n); err == nil {
			f.Since = n
		}
	}
	sw, ok := newSSE(w)
	if !ok {
		writeErr(w, 500, "internal", "streaming unsupported")
		return
	}
	backlog, sub := th.hub.subscribe(f)
	defer th.hub.unsubscribe(sub)
	for _, e := range backlog {
		if sw.event(e.EventID, e.Method, e) != nil {
			return
		}
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-sub.ch:
			if !ok {
				return
			}
			if sw.event(e.EventID, e.Method, e) != nil {
				return
			}
		case <-tick.C:
			sw.comment("keepalive")
		case <-r.Context().Done():
			return
		}
	}
}
