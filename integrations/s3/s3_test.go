package s3_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/s3"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// S3_ENDPOINT=http://localhost:9000 [S3_KEY=minioadmin S3_SECRET=minioadmin] go test ./integrations/s3
func client(t *testing.T) (*s3.Client, string) {
	ep := os.Getenv("S3_ENDPOINT")
	if ep == "" {
		t.Skip("S3_ENDPOINT not set")
	}
	key, secret := os.Getenv("S3_KEY"), os.Getenv("S3_SECRET")
	if key == "" {
		key, secret = "minioadmin", "minioadmin"
	}
	c := &s3.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: key, SecretAccessKey: secret}}
	bucket := fmt.Sprintf("lcg-%d", time.Now().UnixNano())
	if err := c.EnsureBucket(context.Background(), bucket); err != nil {
		t.Fatal(err)
	}
	return c, bucket
}

func TestClient(t *testing.T) {
	c, b := client(t)
	ctx := context.Background()
	if err := c.Put(ctx, b, "dir/a b+c.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(ctx, b, "dir/a b+c.txt")
	if err != nil || string(got) != "hello" {
		t.Fatal(string(got), err)
	}
	if _, err := c.Get(ctx, b, "nope"); !s3.IsNotFound(err) {
		t.Fatal(err)
	}
	for i := range 3 {
		_ = c.Put(ctx, b, fmt.Sprintf("dir/k%d", i), []byte("x"))
	}
	objs, err := c.List(ctx, b, "dir/")
	if err != nil || len(objs) != 4 {
		t.Fatal(objs, err)
	}
	if err := c.Delete(ctx, b, "dir/k0"); err != nil {
		t.Fatal(err)
	}
}

func TestSaver(t *testing.T) { c, b := client(t); conformance.Saver(t, s3.NewSaver(c, b, "ckpt/")) }
func TestStore(t *testing.T) { c, b := client(t); conformance.Store(t, s3.NewStore(c, b, "mem/")) }

func TestLoader(t *testing.T) {
	c, b := client(t)
	ctx := context.Background()
	_ = c.Put(ctx, b, "docs/a.txt", []byte("alpha"))
	_ = c.Put(ctx, b, "docs/b.txt", []byte("beta"))
	docs, err := s3.Loader(c, b, "docs/").Load(ctx)
	if err != nil || len(docs) != 2 || docs[0].PageContent != "alpha" {
		t.Fatal(docs, err)
	}
}
