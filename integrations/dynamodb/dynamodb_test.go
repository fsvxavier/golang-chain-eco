package dynamodb_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/dynamodb"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// DYNAMODB_ENDPOINT=http://localhost:8000 go test ./integrations/dynamodb
// (amazon/dynamodb-local; any dummy credentials work).
func client(t *testing.T) *dynamodb.Client {
	ep := os.Getenv("DYNAMODB_ENDPOINT")
	if ep == "" {
		t.Skip("DYNAMODB_ENDPOINT not set")
	}
	return &dynamodb.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: "x", SecretAccessKey: "y"}}
}

func table(p string) string { return fmt.Sprintf("%s_%d", p, time.Now().UnixNano()) }

func TestSaver(t *testing.T) {
	s, err := dynamodb.NewSaver(context.Background(), client(t), table("ckpt"))
	if err != nil {
		t.Fatal(err)
	}
	conformance.Saver(t, s)
}

func TestStore(t *testing.T) {
	s, err := dynamodb.NewStore(context.Background(), client(t), table("store"))
	if err != nil {
		t.Fatal(err)
	}
	conformance.Store(t, s)
}
