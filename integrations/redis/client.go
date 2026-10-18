// Package redis provides a checkpoint.Saver, a store.Store and a
// vectorstore.Backend (RediSearch, i.e. Redis Stack) on a minimal RESP client
// built on the standard library.
//
//	c := redis.New("localhost:6379")
//	saver := redis.NewSaver(c)
//	mem := redis.NewStore(c)
package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Error is an error reply from the server.
type Error string

func (e Error) Error() string { return "redis: " + string(e) }

// Client is a single-connection RESP2 client, safe for concurrent use
// (commands are serialised). It redials after a connection error.
type Client struct {
	Addr     string
	Password string // optional AUTH
	DB       int    // optional SELECT

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

// New returns a client for addr ("host:port").
func New(addr string) *Client { return &Client{Addr: addr} }

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

func (c *Client) dial(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return err
	}
	c.conn, c.rd = conn, bufio.NewReader(conn)
	if c.Password != "" {
		if _, err := c.roundTrip(ctx, "AUTH", c.Password); err != nil {
			c.drop()
			return err
		}
	}
	if c.DB != 0 {
		if _, err := c.roundTrip(ctx, "SELECT", c.DB); err != nil {
			c.drop()
			return err
		}
	}
	return nil
}

func (c *Client) drop() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Do runs one command. Replies map to: string (simple/bulk; nil bulk -> nil),
// int64, []any, nil. Error replies return Error.
func (c *Client) Do(ctx context.Context, args ...any) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.dial(ctx); err != nil {
			return nil, err
		}
	}
	v, err := c.roundTrip(ctx, args...)
	var re Error
	if err != nil && !errors.As(err, &re) {
		c.drop() // protocol/IO failure: connection state unknown
	}
	return v, err
}

func (c *Client) roundTrip(ctx context.Context, args ...any) (any, error) {
	if dl, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(dl)
	} else {
		c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	buf := fmt.Appendf(nil, "*%d\r\n", len(args))
	for _, a := range args {
		var b []byte
		switch x := a.(type) {
		case string:
			b = []byte(x)
		case []byte:
			b = x
		case int:
			b = strconv.AppendInt(nil, int64(x), 10)
		case int64:
			b = strconv.AppendInt(nil, x, 10)
		case float64:
			b = strconv.AppendFloat(nil, x, 'g', -1, 64)
		default:
			b = fmt.Append(nil, x)
		}
		buf = fmt.Appendf(buf, "$%d\r\n", len(b))
		buf = append(append(buf, b...), '\r', '\n')
	}
	if _, err := c.conn.Write(buf); err != nil {
		return nil, err
	}
	return readReply(c.rd)
}

func readReply(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 {
		return nil, errors.New("redis: short reply")
	}
	body := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return nil, Error(body)
	case ':':
		return strconv.ParseInt(body, 10, 64)
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil || n < 0 {
			return nil, err
		}
		b := make([]byte, n+2)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
		return string(b[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil || n < 0 {
			return nil, err
		}
		out := make([]any, n)
		for i := range out {
			// error elements inside arrays (e.g. EXEC) are kept as values
			if out[i], err = readReply(r); err != nil {
				var re Error
				if !errors.As(err, &re) {
					return nil, err
				}
				out[i] = re
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("redis: unknown reply type %q", line[0])
}

func strs(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i], _ = e.(string)
	}
	return out
}
