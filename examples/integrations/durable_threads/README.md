# durable_threads

Conversation state that survives restarts because it lives in a checkpointer (S3-compatible storage when configured).

## What it does

- A counter graph increments `turns` on each run of thread `customer-42`.
- With `S3_ENDPOINT` set, the checkpointer is `s3.NewSaver` (AWS S3, MinIO, Tigris, R2...); without it, an in-memory saver. Run it twice against the same bucket and the count continues where it stopped.

## How to apply it

```go
c := &s3.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: key, SecretAccessKey: secret}}
saver := s3.NewSaver(c, bucket, "threads/")
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Swap the saver for the store that fits your stack: `sqlsaver.New` (SQLite/PostgreSQL), `redis.NewSaver`, `mongo.NewSaver`, `dynamodb.NewSaver`. The graph code does not change.

## Run it

```bash
go run ./examples/integrations/durable_threads
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Start MinIO (see INTEGRATIONS.md, "Testing against real services") and run:
   `S3_ENDPOINT=http://localhost:9000 S3_BUCKET=demo S3_KEY=... S3_SECRET=... go run ./examples/integrations/durable_threads`
2. Run it twice: the second run must print `turns so far: 3` and `4`.
3. For production, use credentials with access to that bucket/prefix only, enable bucket versioning/encryption, and set a retention policy (`DeleteThread` for old conversations).
4. Register custom state types with `checkpoint.Register`; values are stored as JSON.
5. Choose the backend by access pattern: S3 for cheap archival threads, Redis or SQL for low-latency chat.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
