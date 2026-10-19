package agentprotocol

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // mandated by RFC 6455 for the handshake, not a security use
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A deliberately small WebSocket (RFC 6455) server side: text frames,
// fragmentation, ping/pong and close; no extensions or compression. It is
// enough for the streaming protocol, which is JSON text in both directions.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const maxWSMessage = 16 << 20

type wsConn struct {
	c    net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
	s    *Server
	subs map[string]*wsSub
	smu  sync.Mutex
	next int
}

type wsSub struct {
	sub    *subscription
	h      *hub
	cancel context.CancelFunc
}

func (s *Server) threadWS(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !headerHas(r.Header, "Connection", "upgrade") {
		writeErr(w, 426, "upgrade_required", "this endpoint is a WebSocket; use POST for Server-Sent Events")
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || r.Header.Get("Sec-WebSocket-Version") != "13" {
		writeErr(w, 400, "invalid_request", "missing Sec-WebSocket-Key or unsupported version")
		return
	}
	th := s.threadFor(r.Context(), r.PathValue("thread_id"), true)
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeErr(w, 500, "internal", "websocket unsupported")
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec
	fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	if err := brw.Flush(); err != nil {
		conn.Close()
		return
	}
	wc := &wsConn{c: conn, br: brw.Reader, s: s, subs: map[string]*wsSub{}}
	s.serveWS(context.WithoutCancel(r.Context()), wc, th)
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

func (s *Server) serveWS(ctx context.Context, wc *wsConn, th *threadRec) {
	defer wc.close()
	for {
		op, payload, err := wc.read()
		if err != nil {
			return
		}
		switch op {
		case 0x8:
			_ = wc.write(0x8, payload)
			return
		case 0x9:
			_ = wc.write(0xA, payload)
		case 0xA:
		case 0x1, 0x2:
			var cmd Command
			if err := json.Unmarshal(payload, &cmd); err != nil {
				_ = wc.send(response{Type: "error", Error: "invalid_argument", Message: "invalid JSON command"})
				continue
			}
			_ = wc.send(s.respond(ctx, th, cmd, wc))
		}
	}
}

func (c *wsConn) close() {
	c.smu.Lock()
	for _, s := range c.subs {
		s.cancel()
		s.h.unsubscribe(s.sub)
	}
	c.subs = map[string]*wsSub{}
	c.smu.Unlock()
	c.c.Close()
}

func (c *wsConn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.write(0x1, b)
}

func (c *wsConn) write(op byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	hdr := []byte{0x80 | op}
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	_ = c.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := c.c.Write(append(hdr, payload...)); err != nil {
		return err
	}
	return nil
}

// read returns the next complete message, reassembling fragments. Control
// frames may arrive between fragments and are returned immediately.
func (c *wsConn) read() (op byte, payload []byte, err error) {
	var msg []byte
	var msgOp byte
	for {
		b0, err := c.br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		b1, err := c.br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		fin, opcode := b0&0x80 != 0, b0&0x0F
		if b1&0x80 == 0 {
			return 0, nil, errors.New("ws: client frames must be masked")
		}
		n := uint64(b1 & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxWSMessage || uint64(len(msg))+n > maxWSMessage {
			return 0, nil, errors.New("ws: message too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(c.br, data); err != nil {
			return 0, nil, err
		}
		for i := range data {
			data[i] ^= mask[i%4]
		}
		if opcode >= 0x8 { // control frame
			return opcode, data, nil
		}
		if opcode != 0 {
			msgOp = opcode
		}
		msg = append(msg, data...)
		if fin {
			return msgOp, msg, nil
		}
	}
}

// subscriptionCommand implements subscription.subscribe/unsubscribe/reconnect.
func (c *wsConn) subscriptionCommand(th *threadRec, cmd Command) (any, error) {
	switch cmd.Method {
	case "subscription.subscribe":
		var f Filter
		if err := parse(cmd.Params, &f); err != nil {
			return nil, err
		}
		if len(f.Channels) == 0 {
			return nil, cerr("invalid_argument", "channels must contain at least one channel")
		}
		id, replayed := c.subscribe(th, f)
		return map[string]any{"subscriptionId": id, "replayedEvents": replayed}, nil
	case "subscription.unsubscribe":
		var p struct {
			SubscriptionID string `json:"subscriptionId"`
		}
		if err := parse(cmd.Params, &p); err != nil {
			return nil, err
		}
		c.smu.Lock()
		sub := c.subs[p.SubscriptionID]
		delete(c.subs, p.SubscriptionID)
		c.smu.Unlock()
		if sub == nil {
			return nil, cerr("no_such_subscription", "no subscription "+p.SubscriptionID)
		}
		sub.cancel()
		sub.h.unsubscribe(sub.sub)
		return nil, nil
	case "subscription.reconnect":
		var p struct {
			LastEventID   string   `json:"lastEventId"`
			Subscriptions []string `json:"subscriptions"`
		}
		if err := parse(cmd.Params, &p); err != nil {
			return nil, err
		}
		var since uint64
		if p.LastEventID != "" {
			if _, err := fmt.Sscanf(p.LastEventID, "evt_%d", &since); err != nil {
				return nil, cerr("invalid_argument", "lastEventId must look like evt_<n>")
			}
		}
		c.smu.Lock()
		var filters []Filter
		for _, id := range p.Subscriptions {
			if s := c.subs[id]; s != nil {
				filters = append(filters, s.sub.f)
			}
		}
		c.smu.Unlock()
		missed := uint64(0)
		for _, f := range filters {
			f.Since = since
			_, n := c.subscribe(th, f)
			missed += uint64(n)
		}
		return map[string]any{"restored": len(filters) > 0, "missedEvents": missed}, nil
	}
	return nil, cerr("unknown_command", cmd.Method)
}

// subscribe registers a subscription and pumps its events to the socket.
func (c *wsConn) subscribe(th *threadRec, f Filter) (string, int) {
	backlog, sub := th.hub.subscribe(f)
	c.smu.Lock()
	c.next++
	id := fmt.Sprintf("sub_%d", c.next)
	ctx, cancel := context.WithCancel(context.Background())
	c.subs[id] = &wsSub{sub: sub, h: th.hub, cancel: cancel}
	c.smu.Unlock()
	go func() {
		for _, e := range backlog {
			if c.send(e) != nil {
				return
			}
		}
		for {
			select {
			case e, ok := <-sub.ch:
				if !ok || c.send(e) != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return id, len(backlog)
}
