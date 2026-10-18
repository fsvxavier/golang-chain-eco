package kubernetes

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ExecResult is the outcome of Exec.
type ExecResult struct {
	Output   string // stdout and stderr interleaved in arrival order
	ExitCode int
}

// Exec runs argv in the pod's container (empty = the pod's first container)
// over the exec WebSocket (subprotocol v4.channel.k8s.io) and waits for it.
func (c *Client) Exec(ctx context.Context, namespace, pod, container string, argv []string, maxOutput int) (ExecResult, error) {
	c.init()
	u, err := url.Parse(c.Server)
	if err != nil {
		return ExecResult{}, err
	}
	q := url.Values{"stdout": {"true"}, "stderr": {"true"}}
	for _, a := range argv {
		q.Add("command", a)
	}
	if container != "" {
		q.Set("container", container)
	}
	target := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(pod) + "/exec?" + q.Encode()

	host := u.Host
	if u.Port() == "" {
		host += map[string]string{"https": ":443", "http": ":80"}[u.Scheme]
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if u.Scheme == "https" {
		cfg := c.tlsc.Clone()
		cfg.ServerName = u.Hostname()
		conn, err = (&tls.Dialer{NetDialer: &d, Config: cfg}).DialContext(ctx, "tcp", host)
	} else {
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return ExecResult{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	key := make([]byte, 16)
	rand.Read(key)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: v4.channel.k8s.io\r\nAuthorization: Bearer %s\r\n\r\n",
		target, u.Host, base64.StdEncoding.EncodeToString(key), c.Token)
	rd := bufio.NewReader(conn)
	resp, err := http.ReadResponse(rd, nil)
	if err != nil {
		return ExecResult{}, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ExecResult{}, &StatusError{resp.StatusCode, strings.TrimSpace(string(raw))}
	}

	var out strings.Builder
	res := ExecResult{ExitCode: -1}
	for {
		op, payload, err := readFrame(rd)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return res, err
		}
		switch op {
		case 0x8: // close
			res.Output = out.String()
			return finish(res)
		case 0x9: // ping -> pong (client frames must be masked)
			_ = writeMasked(conn, 0xA, payload)
		case 0x1, 0x2:
			if len(payload) == 0 {
				continue
			}
			switch payload[0] {
			case 1, 2: // stdout, stderr
				if maxOutput <= 0 || out.Len() < maxOutput {
					out.Write(payload[1:])
				}
			case 3: // error channel: metav1.Status
				res.ExitCode = parseStatus(payload[1:])
			}
		}
	}
	res.Output = out.String()
	return finish(res)
}

func finish(r ExecResult) (ExecResult, error) {
	if r.ExitCode == -1 {
		return r, fmt.Errorf("kubernetes: exec ended without a status")
	}
	return r, nil
}

// parseStatus reads a v4 error-channel Status: Success => 0, otherwise the
// ExitCode cause, or 1 for other failures.
func parseStatus(b []byte) int {
	var st struct {
		Status  string
		Message string
		Details struct {
			Causes []struct{ Reason, Message string }
		}
	}
	if json.Unmarshal(b, &st) != nil {
		return 1
	}
	if st.Status == "Success" {
		return 0
	}
	for _, c := range st.Details.Causes {
		if c.Reason == "ExitCode" {
			if n, err := strconv.Atoi(c.Message); err == nil {
				return n
			}
		}
	}
	return 1
}

func readFrame(r *bufio.Reader) (op byte, payload []byte, err error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err = io.ReadFull(r, h[:]); err != nil {
			return 0, nil, err
		}
		fin, o := h[0]&0x80 != 0, h[0]&0x0F
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err = io.ReadFull(r, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err = io.ReadFull(r, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 64<<20 {
			return 0, nil, fmt.Errorf("kubernetes: websocket frame too large")
		}
		var mask [4]byte
		masked := h[1]&0x80 != 0
		if masked {
			if _, err = io.ReadFull(r, mask[:]); err != nil {
				return 0, nil, err
			}
		}
		buf := make([]byte, n)
		if _, err = io.ReadFull(r, buf); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range buf {
				buf[i] ^= mask[i%4]
			}
		}
		if o != 0 { // not a continuation: first frame of a message (or control)
			op = o
		}
		msg = append(msg, buf...)
		if fin {
			return op, msg, nil
		}
	}
}

func writeMasked(w io.Writer, op byte, payload []byte) error {
	frame := []byte{0x80 | op}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, 0x80|byte(n))
	case n < 65536:
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	default:
		frame = binary.BigEndian.AppendUint64(append(frame, 0x80|127), uint64(n))
	}
	var mask [4]byte
	rand.Read(mask[:])
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.Write(frame)
	return err
}
