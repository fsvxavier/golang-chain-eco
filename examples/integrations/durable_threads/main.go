// durable_threads: a conversation counter whose state survives restarts by
// living in a checkpointer. With S3_ENDPOINT set it uses the S3 saver (any
// S3-compatible service: AWS, MinIO, Tigris, R2...); without it, an
// in-memory saver so the example still runs. Run it twice against the same
// bucket and the count continues where it stopped.
//
//	S3_ENDPOINT=http://localhost:9000 S3_BUCKET=demo S3_KEY=... S3_SECRET=... go run ./examples/integrations/durable_threads
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/integrations/s3"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

func saver(ctx context.Context) (checkpoint.Saver, string) {
	ep := os.Getenv("S3_ENDPOINT")
	if ep == "" {
		return checkpoint.NewMemory(), "in-memory (set S3_ENDPOINT for durable storage)"
	}
	c := &s3.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: os.Getenv("S3_KEY"), SecretAccessKey: os.Getenv("S3_SECRET")}}
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		bucket = "golang-chain-eco-demo"
	}
	if err := c.EnsureBucket(ctx, bucket); err != nil {
		log.Fatal(err)
	}
	return s3.NewSaver(c, bucket, "threads/"), "s3://" + bucket + "/threads/"
}

func main() {
	ctx := context.Background()
	sv, where := saver(ctx)
	fmt.Println("checkpoints:", where)

	app, err := graph.NewStateGraph(graph.Schema{"turns": graph.Sum()}).
		AddNode("count", func(context.Context, graph.State) (any, error) { return graph.State{"turns": 1}, nil }).
		AddEdge(graph.Start, "count").AddEdge("count", graph.End).
		Compile(graph.WithCheckpointer(sv))
	if err != nil {
		log.Fatal(err)
	}
	cfg := runnables.WithConfig(ctx, graph.Thread("customer-42"))
	for range 2 {
		out, err := app.Invoke(cfg, graph.State{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("turns so far:", out["turns"])
	}
}
