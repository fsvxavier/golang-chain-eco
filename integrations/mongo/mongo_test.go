package mongo_test

import (
	"context"
	"os"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/mongo"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// MONGO_ADDR=localhost:27017 [MONGO_USER=.. MONGO_PASS=..] go test ./integrations/mongo
func client(t *testing.T) *mongo.Client {
	addr := os.Getenv("MONGO_ADDR")
	if addr == "" {
		t.Skip("MONGO_ADDR not set")
	}
	c, err := mongo.Connect(context.Background(), mongo.Options{Addr: addr, Database: "lcgtest",
		Username: os.Getenv("MONGO_USER"), Password: os.Getenv("MONGO_PASS")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSaver(t *testing.T) { conformance.Saver(t, mongo.NewSaver(client(t))) }
func TestStore(t *testing.T) { conformance.Store(t, mongo.NewStore(client(t))) }
func TestVector(t *testing.T) {
	c := client(t)
	c.Command(context.Background(), mongo.D{{"drop", "vectors"}})
	conformance.Vector(t, &mongo.Vector{C: c})
}
