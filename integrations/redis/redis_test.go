package redis_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/redis"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// Needs a Redis Stack server: REDIS_ADDR=localhost:6379 go test ./integrations/redis
func client(t *testing.T) *redis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	c := redis.New(addr)
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSaver(t *testing.T) { conformance.Saver(t, redis.NewSaver(client(t))) }
func TestStore(t *testing.T) { conformance.Store(t, redis.NewStore(client(t))) }
func TestVector(t *testing.T) {
	conformance.Vector(t, &redis.Vector{C: client(t), Index: "lcg_test", Prefix: "lcgtest:"})
}

// FALKORDB_ADDR=localhost:6379 (falkordb/falkordb image)
func TestFalkorDB(t *testing.T) {
	addr := os.Getenv("FALKORDB_ADDR")
	if addr == "" {
		t.Skip("FALKORDB_ADDR not set")
	}
	c := redis.New(addr)
	defer c.Close()
	conformance.Vector(t, &redis.FalkorDB{C: c, Graph: "lcgtest" + fmt.Sprint(time.Now().UnixNano())})
}
