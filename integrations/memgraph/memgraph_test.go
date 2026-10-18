package memgraph

import (
	"os"
	"reflect"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

func TestPackStreamRoundTrip(t *testing.T) {
	in := map[string]any{"s": "hello", "long": string(make([]byte, 300)), "i": int64(-5), "big": int64(1 << 40), "mid": int64(40000),
		"f": 1.5, "t": true, "n": nil, "l": []any{int64(1), "x", 2.5}, "m": map[string]any{"k": int64(7)}}
	b, err := pack(nil, in)
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := unpack(b)
	if err != nil || n != len(b) || !reflect.DeepEqual(out, in) {
		t.Fatalf("%v %d/%d %v", out, n, len(b), err)
	}
	if fl, _, _ := unpack(mustPack(t, []float32{0.5, 1})); !reflect.DeepEqual(fl, []any{0.5, 1.0}) {
		t.Fatal(fl)
	}
}

func mustPack(t *testing.T, v any) []byte {
	b, err := pack(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// MEMGRAPH_ADDR=localhost:7687 (memgraph/memgraph, no auth)
func TestVectorReal(t *testing.T) {
	addr := os.Getenv("MEMGRAPH_ADDR")
	if addr == "" {
		t.Skip("MEMGRAPH_ADDR not set")
	}
	c := &Client{Addr: addr, Username: os.Getenv("MEMGRAPH_USER"), Password: os.Getenv("MEMGRAPH_PASS")}
	defer c.Close()
	conformance.Vector(t, &Vector{C: c})
}
