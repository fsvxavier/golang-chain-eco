// Package mongo provides a checkpoint.Saver, a store.Store and a
// vectorstore.Backend on MongoDB (including Atlas), speaking the wire
// protocol (OP_MSG) directly with a small BSON codec, so it needs no
// dependency. Supported: plain and TLS connections, SCRAM-SHA-256 auth.
// Not supported: mongodb+srv:// discovery (pass a host:port), compression,
// replica-set failover, sessions/transactions.
//
//	c, _ := mongo.Connect(ctx, mongo.Options{Addr: "localhost:27017", Database: "app"})
//	saver := mongo.NewSaver(c)
package mongo

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options configure Connect.
type Options struct {
	Addr       string // host:port
	Database   string // default "langchain"
	Username   string // optional (SCRAM-SHA-256)
	Password   string
	AuthSource string // default "admin"
	TLS        *tls.Config
}

// Client is a single-connection client, safe for concurrent use.
type Client struct {
	opt Options
	req atomic.Int32

	mu   sync.Mutex
	conn net.Conn
}

// CommandError is a failed command (ok: 0) or write error.
type CommandError struct {
	Code int
	Msg  string
}

func (e *CommandError) Error() string { return fmt.Sprintf("mongo: %s (code %d)", e.Msg, e.Code) }

// Connect dials and authenticates.
func Connect(ctx context.Context, o Options) (*Client, error) {
	if o.Database == "" {
		o.Database = "langchain"
	}
	if o.AuthSource == "" {
		o.AuthSource = "admin"
	}
	c := &Client{opt: o}
	if err := c.dial(ctx); err != nil {
		return nil, err
	}
	return c, nil
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

func (c *Client) dial(ctx context.Context) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if c.opt.TLS != nil {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: c.opt.TLS}).DialContext(ctx, "tcp", c.opt.Addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", c.opt.Addr)
	}
	if err != nil {
		return err
	}
	c.conn = conn
	if c.opt.Username != "" {
		if err := c.scram(ctx); err != nil {
			conn.Close()
			c.conn = nil
			return err
		}
	}
	return nil
}

// Command runs a command against the configured database.
func (c *Client) Command(ctx context.Context, cmd D) (M, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.dial(ctx); err != nil {
			return nil, err
		}
	}
	res, err := c.run(ctx, c.opt.Database, cmd)
	if err != nil {
		if _, ok := err.(*CommandError); !ok {
			c.conn.Close() // IO/protocol failure
			c.conn = nil
		}
	}
	return res, err
}

func (c *Client) run(ctx context.Context, db string, cmd D) (M, error) {
	body, err := marshal(append(cmd, E{"$db", db}))
	if err != nil {
		return nil, err
	}
	msg := make([]byte, 16, 16+5+len(body))
	binary.LittleEndian.PutUint32(msg[4:], uint32(c.req.Add(1)))
	binary.LittleEndian.PutUint32(msg[12:], 2013) // OP_MSG
	msg = append(msg, 0, 0, 0, 0, 0)              // flagBits, section kind 0
	msg = append(msg, body...)
	binary.LittleEndian.PutUint32(msg, uint32(len(msg)))

	if dl, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(dl)
	} else {
		c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	if _, err := c.conn.Write(msg); err != nil {
		return nil, err
	}
	var hdr [16]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint32(hdr[:]))
	if n < 21 || n > 48<<20 {
		return nil, fmt.Errorf("mongo: bad reply length %d", n)
	}
	buf := make([]byte, n-16)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(hdr[12:]) != 2013 || buf[4] != 0 {
		return nil, fmt.Errorf("mongo: unexpected reply (opcode %d)", binary.LittleEndian.Uint32(hdr[12:]))
	}
	doc, _, err := decodeDoc(buf[5:])
	if err != nil {
		return nil, err
	}
	if num(doc["ok"]) != 1 {
		msg, _ := doc["errmsg"].(string)
		return nil, &CommandError{Code: int(num(doc["code"])), Msg: msg}
	}
	if we, ok := doc["writeErrors"].([]any); ok && len(we) > 0 {
		m, _ := we[0].(M)
		msg, _ := m["errmsg"].(string)
		return nil, &CommandError{Code: int(num(m["code"])), Msg: msg}
	}
	return doc, nil
}

// find runs a find and drains the cursor.
func (c *Client) find(ctx context.Context, coll string, filter M, sort D, limit int) ([]M, error) {
	cmd := D{{"find", coll}, {"filter", filter}}
	if len(sort) > 0 {
		cmd = append(cmd, E{"sort", sort})
	}
	if limit > 0 {
		cmd = append(cmd, E{"limit", int64(limit)}, E{"singleBatch", false})
	}
	res, err := c.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	var out []M
	for {
		cur, _ := res["cursor"].(M)
		batch, _ := cur["firstBatch"].([]any)
		if batch == nil {
			batch, _ = cur["nextBatch"].([]any)
		}
		for _, d := range batch {
			out = append(out, d.(M))
		}
		id, _ := cur["id"].(int64)
		if id == 0 || (limit > 0 && len(out) >= limit) {
			return out, nil
		}
		if res, err = c.Command(ctx, D{{"getMore", id}, {"collection", coll}}); err != nil {
			return nil, err
		}
	}
}

// --- SCRAM-SHA-256 ---------------------------------------------------------

func hmacSum(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// pbkdf2 (HMAC-SHA256); crypto/pbkdf2 only exists from Go 1.24.
func pbkdf2(pass, salt []byte, iter int) []byte {
	m := hmac.New(sha256.New, pass)
	m.Write(salt)
	m.Write([]byte{0, 0, 0, 1})
	u := m.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iter; i++ {
		m.Reset()
		m.Write(u)
		u = m.Sum(nil)
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}

func scramFields(s string) map[string]string {
	out := map[string]string{}
	for _, p := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[k] = v
		}
	}
	return out
}

func (c *Client) scram(ctx context.Context) error {
	nb := make([]byte, 24)
	rand.Read(nb)
	nonce := base64.StdEncoding.EncodeToString(nb)
	bare := "n=" + strings.NewReplacer("=", "=3D", ",", "=2C").Replace(c.opt.Username) + ",r=" + nonce
	res, err := c.run(ctx, c.opt.AuthSource, D{{"saslStart", int32(1)}, {"mechanism", "SCRAM-SHA-256"},
		{"payload", []byte("n,," + bare)}, {"autoAuthorize", int32(1)}, {"options", D{{"skipEmptyExchange", true}}}})
	if err != nil {
		return err
	}
	serverFirst := string(res["payload"].([]byte))
	f := scramFields(serverFirst)
	iter, _ := strconv.Atoi(f["i"])
	salt, err := base64.StdEncoding.DecodeString(f["s"])
	if err != nil || iter < 1 || !strings.HasPrefix(f["r"], nonce) {
		return fmt.Errorf("mongo: bad SCRAM server-first message")
	}
	salted := pbkdf2([]byte(c.opt.Password), salt, iter)
	clientKey := hmacSum(salted, "Client Key")
	stored := sha256.Sum256(clientKey)
	withoutProof := "c=biws,r=" + f["r"]
	authMsg := bare + "," + serverFirst + "," + withoutProof
	sig := hmacSum(stored[:], authMsg)
	proof := make([]byte, len(sig))
	for i := range proof {
		proof[i] = clientKey[i] ^ sig[i]
	}
	res, err = c.run(ctx, c.opt.AuthSource, D{{"saslContinue", int32(1)}, {"conversationId", res["conversationId"]},
		{"payload", []byte(withoutProof + ",p=" + base64.StdEncoding.EncodeToString(proof))}})
	if err != nil {
		return err
	}
	want := base64.StdEncoding.EncodeToString(hmacSum(hmacSum(salted, "Server Key"), authMsg))
	if subtle.ConstantTimeCompare([]byte(scramFields(string(res["payload"].([]byte)))["v"]), []byte(want)) != 1 {
		return fmt.Errorf("mongo: server SCRAM signature mismatch")
	}
	if done, _ := res["done"].(bool); !done {
		_, err = c.run(ctx, c.opt.AuthSource, D{{"saslContinue", int32(1)}, {"conversationId", res["conversationId"]}, {"payload", []byte{}}})
	}
	return err
}
