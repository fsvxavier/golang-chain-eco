# Development server (`langgraph dev`)

**English** | [Português](../pt-BR/DEV.md) | [Español](../es-ES/DEV.md)

Serves compiled graphs over the Agent Protocol (`agentprotocol/`), with request logging and the URL printed.
It is not Python's `langgraph dev`: Go does not load code at runtime, so your graph enters through a `main` of your own.

## Demo

```bash
go run ./cmd/langgraph dev --addr 127.0.0.1:2024
```

Serves the `echo` agent (a fake model that calls a tool). No API key needed.

## Your graph

```go
package main

import (
	"log"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/dev"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	g := buildGraph(graph.WithCheckpointer(checkpoint.NewMemory())) // your graph, compiled with a checkpointer
	log.Fatal(dev.Serve("127.0.0.1:2024", map[string]*graph.Compiled{"my_agent": g}))
}
```

- The map key is the `assistant_id` used by clients.
- Without a checkpointer the server starts, but logs a warning: threads do not keep state.
- Ctrl-C stops the process; there is no draining of requests in progress.
