# Servidor de desenvolvimento (`langgraph dev`)

[English](../en-US/DEV.md) | **Português** | [Español](../es-ES/DEV.md)

Sobe grafos compilados pelo protocolo Agent Protocol (`agentprotocol/`), com log de requisições e URL impressa.
Não é o `langgraph dev` do Python: o Go não carrega código em runtime, então o seu grafo entra por um `main` seu.

## Demo

```bash
go run ./cmd/langgraph dev --addr 127.0.0.1:2024
```

Serve o agent `echo` (modelo fake que chama uma tool). Não precisa de chave de API.

## Seu grafo

```go
package main

import (
	"log"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/dev"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	g := buildGraph(graph.WithCheckpointer(checkpoint.NewMemory())) // seu grafo, compilado com checkpointer
	log.Fatal(dev.Serve("127.0.0.1:2024", map[string]*graph.Compiled{"meu_agent": g}))
}
```

- A chave do mapa é o `assistant_id` usado pelos clientes.
- Sem checkpointer o servidor sobe, mas loga um aviso: threads não guardam estado.
- Ctrl-C encerra o processo; não há drenagem de requisições em andamento.
