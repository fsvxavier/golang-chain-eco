# Servidor de desarrollo (`langgraph dev`)

[English](../en-US/DEV.md) | [Português](../pt-BR/DEV.md) | **Español**

Levanta grafos compilados mediante el protocolo Agent Protocol (`agentprotocol/`), con registro de solicitudes y la URL impresa.
No es el `langgraph dev` de Python: Go no carga código en tiempo de ejecución, así que tu grafo entra por un `main` propio.

## Demo

```bash
go run ./cmd/langgraph dev --addr 127.0.0.1:2024
```

Sirve el agent `echo` (modelo falso que llama a una tool). No necesita clave de API.

## Tu grafo

```go
package main

import (
	"log"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/dev"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	g := buildGraph(graph.WithCheckpointer(checkpoint.NewMemory())) // tu grafo, compilado con checkpointer
	log.Fatal(dev.Serve("127.0.0.1:2024", map[string]*graph.Compiled{"mi_agent": g}))
}
```

- La clave del mapa es el `assistant_id` que usan los clientes.
- Sin checkpointer el servidor arranca, pero registra un aviso: los threads no guardan estado.
- Ctrl-C termina el proceso; no hay drenaje de las solicitudes en curso.
