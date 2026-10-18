// Package memgraph provides a minimal Bolt client (PackStream over TCP,
// protocol 4.x) and a vectorstore.Backend on Memgraph's native vector
// index. Bolt is the only protocol Memgraph speaks, so the client is
// implemented here with the standard library. Only what the backend needs is
// supported: basic auth, auto-commit queries, no transactions, no routing.
package memgraph

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"sync"
	"time"
)

// Client is a single-connection Bolt client, safe for concurrent use.
type Client struct {
	Addr     string // host:port (default port 7687)
	Username string
	Password string

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Error is a Bolt FAILURE reply.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return "memgraph: " + e.Code + ": " + e.Message }

func (c *Client) connect(ctx context.Context) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return err
	}
	// magic + up to four proposed versions (each: 0, range, minor, major)
	hs := []byte{0x60, 0x60, 0xB0, 0x17, 0, 0, 4, 4, 0, 0, 1, 4, 0, 0, 0, 4, 0, 0, 0, 1}
	if _, err := conn.Write(hs); err != nil {
		conn.Close()
		return err
	}
	var v [4]byte
	if _, err := io.ReadFull(conn, v[:]); err != nil {
		conn.Close()
		return err
	}
	if v[3] != 4 && v[3] != 1 {
		conn.Close()
		return fmt.Errorf("memgraph: server speaks unsupported Bolt %d.%d", v[3], v[2])
	}
	c.conn, c.rd = conn, bufio.NewReader(conn)
	extra := map[string]any{"user_agent": "golang-chain-eco/1.0", "scheme": "basic", "principal": c.Username, "credentials": c.Password}
	if c.Username == "" {
		extra = map[string]any{"user_agent": "golang-chain-eco/1.0", "scheme": "none"}
	}
	if _, _, err := c.exchange(0x01, []any{extra}, false); err != nil {
		conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// Run executes a query and returns column names and rows.
func (c *Client) Run(ctx context.Context, query string, params map[string]any) ([]string, [][]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			return nil, nil, err
		}
	}
	if dl, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(dl)
	} else {
		c.conn.SetDeadline(time.Now().Add(60 * time.Second))
	}
	if params == nil {
		params = map[string]any{}
	}
	meta, _, err := c.exchange(0x10, []any{query, params, map[string]any{}}, false)
	if err != nil {
		c.reset()
		return nil, nil, err
	}
	var cols []string
	if f, ok := meta["fields"].([]any); ok {
		for _, x := range f {
			s, _ := x.(string)
			cols = append(cols, s)
		}
	}
	_, rows, err := c.exchange(0x3F, []any{map[string]any{"n": int64(-1)}}, true)
	if err != nil {
		c.reset()
	}
	return cols, rows, err
}

// reset clears the failed state (RESET) so the connection stays usable.
func (c *Client) reset() {
	if _, _, err := c.exchange(0x0F, nil, false); err != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// exchange sends one message and reads replies up to the terminal SUCCESS/FAILURE.
func (c *Client) exchange(sig byte, fields []any, collect bool) (map[string]any, [][]any, error) {
	if err := c.send(sig, fields); err != nil {
		return nil, nil, err
	}
	var rows [][]any
	for {
		s, f, err := c.recv()
		if err != nil {
			return nil, nil, err
		}
		switch s {
		case 0x71: // RECORD
			if collect && len(f) > 0 {
				row, _ := f[0].([]any)
				rows = append(rows, row)
			}
		case 0x70: // SUCCESS
			m := map[string]any{}
			if len(f) > 0 {
				m, _ = f[0].(map[string]any)
			}
			return m, rows, nil
		case 0x7F: // FAILURE
			e := &Error{}
			if len(f) > 0 {
				m, _ := f[0].(map[string]any)
				e.Code, _ = m["code"].(string)
				e.Message, _ = m["message"].(string)
			}
			return nil, nil, e
		case 0x7E: // IGNORED
			return nil, nil, fmt.Errorf("memgraph: request ignored (connection in failed state)")
		}
	}
}

func (c *Client) send(sig byte, fields []any) error {
	body := []byte{0xB0 | byte(len(fields)), sig}
	for _, f := range fields {
		var err error
		if body, err = pack(body, f); err != nil {
			return err
		}
	}
	var out []byte
	for len(body) > 0 {
		n := min(len(body), 0xFFFF)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
		out = append(out, body[:n]...)
		body = body[n:]
	}
	out = append(out, 0, 0)
	_, err := c.conn.Write(out)
	return err
}

func (c *Client) recv() (byte, []any, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.rd, h[:]); err != nil {
			return 0, nil, err
		}
		n := int(binary.BigEndian.Uint16(h[:]))
		if n == 0 {
			if len(msg) == 0 {
				continue // NOOP chunk (keep-alive)
			}
			break
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(c.rd, buf); err != nil {
			return 0, nil, err
		}
		msg = append(msg, buf...)
	}
	v, _, err := unpack(msg)
	if err != nil {
		return 0, nil, err
	}
	st, ok := v.(structure)
	if !ok {
		return 0, nil, fmt.Errorf("memgraph: unexpected reply %T", v)
	}
	return st.sig, st.fields, nil
}

// ---------------------------------------------------------------- PackStream

type structure struct {
	sig    byte
	fields []any
}

func packSize(b []byte, n int, tiny, b8, b16, b32 byte) []byte {
	switch {
	case n < 16 && tiny != 0:
		return append(b, tiny|byte(n))
	case n < 256:
		return append(b, b8, byte(n))
	case n < 65536:
		return binary.BigEndian.AppendUint16(append(b, b16), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, b32), uint32(n))
}

func pack(b []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(b, 0xC0), nil
	case bool:
		if x {
			return append(b, 0xC3), nil
		}
		return append(b, 0xC2), nil
	case int:
		return pack(b, int64(x))
	case int32:
		return pack(b, int64(x))
	case int64:
		switch {
		case x >= -16 && x <= 127:
			return append(b, byte(int8(x))), nil
		case x >= math.MinInt8 && x <= math.MaxInt8:
			return append(b, 0xC8, byte(int8(x))), nil
		case x >= math.MinInt16 && x <= math.MaxInt16:
			return binary.BigEndian.AppendUint16(append(b, 0xC9), uint16(int16(x))), nil
		case x >= math.MinInt32 && x <= math.MaxInt32:
			return binary.BigEndian.AppendUint32(append(b, 0xCA), uint32(int32(x))), nil
		}
		return binary.BigEndian.AppendUint64(append(b, 0xCB), uint64(x)), nil
	case float32:
		return pack(b, float64(x))
	case float64:
		return binary.BigEndian.AppendUint64(append(b, 0xC1), math.Float64bits(x)), nil
	case string:
		return append(packSize(b, len(x), 0x80, 0xD0, 0xD1, 0xD2), x...), nil
	case []byte:
		return append(packSize(b, len(x), 0, 0xCC, 0xCD, 0xCE), x...), nil
	case []float32:
		b = packSize(b, len(x), 0x90, 0xD4, 0xD5, 0xD6)
		for _, f := range x {
			b, _ = pack(b, float64(f))
		}
		return b, nil
	case []string:
		b = packSize(b, len(x), 0x90, 0xD4, 0xD5, 0xD6)
		for _, s := range x {
			b, _ = pack(b, s)
		}
		return b, nil
	case []any:
		b = packSize(b, len(x), 0x90, 0xD4, 0xD5, 0xD6)
		for _, e := range x {
			var err error
			if b, err = pack(b, e); err != nil {
				return nil, err
			}
		}
		return b, nil
	case []map[string]any:
		a := make([]any, len(x))
		for i, m := range x {
			a[i] = m
		}
		return pack(b, a)
	case map[string]any:
		b = packSize(b, len(x), 0xA0, 0xD8, 0xD9, 0xDA)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b, _ = pack(b, k)
			var err error
			if b, err = pack(b, x[k]); err != nil {
				return nil, err
			}
		}
		return b, nil
	}
	return nil, fmt.Errorf("memgraph: cannot pack %T", v)
}

func unpack(b []byte) (any, int, error) {
	if len(b) == 0 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	m := b[0]
	short := io.ErrUnexpectedEOF
	need := func(n int) bool { return len(b) >= n }
	switch {
	case m <= 0x7F:
		return int64(m), 1, nil
	case m >= 0xF0:
		return int64(int8(m)), 1, nil
	case m >= 0x80 && m <= 0x8F:
		n := int(m & 0x0F)
		if !need(1 + n) {
			return nil, 0, short
		}
		return string(b[1 : 1+n]), 1 + n, nil
	case m >= 0x90 && m <= 0x9F:
		return unpackList(b, int(m&0x0F), 1)
	case m >= 0xA0 && m <= 0xAF:
		return unpackMap(b, int(m&0x0F), 1)
	case m >= 0xB0 && m <= 0xBF:
		n := int(m & 0x0F)
		if !need(2) {
			return nil, 0, short
		}
		l, used, err := unpackN(b[2:], n)
		return structure{b[1], l}, 2 + used, err
	}
	size := func(w int) (int, bool) {
		if !need(1 + w) {
			return 0, false
		}
		switch w {
		case 1:
			return int(b[1]), true
		case 2:
			return int(binary.BigEndian.Uint16(b[1:])), true
		}
		return int(binary.BigEndian.Uint32(b[1:])), true
	}
	switch m {
	case 0xC0:
		return nil, 1, nil
	case 0xC2:
		return false, 1, nil
	case 0xC3:
		return true, 1, nil
	case 0xC1:
		if !need(9) {
			return nil, 0, short
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b[1:])), 9, nil
	case 0xC8:
		if !need(2) {
			return nil, 0, short
		}
		return int64(int8(b[1])), 2, nil
	case 0xC9:
		if !need(3) {
			return nil, 0, short
		}
		return int64(int16(binary.BigEndian.Uint16(b[1:]))), 3, nil
	case 0xCA:
		if !need(5) {
			return nil, 0, short
		}
		return int64(int32(binary.BigEndian.Uint32(b[1:]))), 5, nil
	case 0xCB:
		if !need(9) {
			return nil, 0, short
		}
		return int64(binary.BigEndian.Uint64(b[1:])), 9, nil
	case 0xD0, 0xD1, 0xD2, 0xCC, 0xCD, 0xCE:
		w := map[byte]int{0xD0: 1, 0xD1: 2, 0xD2: 4, 0xCC: 1, 0xCD: 2, 0xCE: 4}[m]
		n, ok := size(w)
		if !ok || !need(1+w+n) {
			return nil, 0, short
		}
		if m >= 0xD0 {
			return string(b[1+w : 1+w+n]), 1 + w + n, nil
		}
		return append([]byte(nil), b[1+w:1+w+n]...), 1 + w + n, nil
	case 0xD4, 0xD5, 0xD6:
		n, ok := size(map[byte]int{0xD4: 1, 0xD5: 2, 0xD6: 4}[m])
		if !ok {
			return nil, 0, short
		}
		return unpackList(b, n, 1+map[byte]int{0xD4: 1, 0xD5: 2, 0xD6: 4}[m])
	case 0xD8, 0xD9, 0xDA:
		n, ok := size(map[byte]int{0xD8: 1, 0xD9: 2, 0xDA: 4}[m])
		if !ok {
			return nil, 0, short
		}
		return unpackMap(b, n, 1+map[byte]int{0xD8: 1, 0xD9: 2, 0xDA: 4}[m])
	}
	return nil, 0, fmt.Errorf("memgraph: unsupported PackStream marker %#x", m)
}

func unpackN(b []byte, n int) ([]any, int, error) {
	out := make([]any, 0, n)
	p := 0
	for range n {
		v, used, err := unpack(b[p:])
		if err != nil {
			return nil, 0, err
		}
		out, p = append(out, v), p+used
	}
	return out, p, nil
}

func unpackList(b []byte, n, off int) (any, int, error) {
	l, used, err := unpackN(b[off:], n)
	return l, off + used, err
}

func unpackMap(b []byte, n, off int) (any, int, error) {
	l, used, err := unpackN(b[off:], 2*n)
	if err != nil {
		return nil, 0, err
	}
	m := make(map[string]any, n)
	for i := 0; i+1 < len(l); i += 2 {
		k, _ := l[i].(string)
		m[k] = l[i+1]
	}
	return m, off + used, nil
}
