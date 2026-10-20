# typed_state

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un grafo cuyo estado es una struct Go con tags de reducer y validación, con un paso de aprobación humana.

## Qué hace

- `OrderState` usa tags `graph` para nombrar canales y reducers (`sum`, `append`, `messages`) e implementa `Validate()`.
- `price` calcula un total, `approve` se pausa con `graph.Interrupt` para una persona, `confirm` finaliza.
- El ejemplo reanuda el thread con `Command{Resume: true}` y muestra que una cantidad negativa es rechazada por `Validate` antes de que corra cualquier nodo.

## Cómo aplicarlo

```go
type OrderState struct {
    Messages []messages.Message `graph:"messages,reducer=messages"`
    Total    int                `graph:"total,reducer=sum"`
    Log      []string           `graph:"log,reducer=append"`
}
g := graph.NewTyped[OrderState]().AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) { ... })
```

Prefiere estado tipado en grafos más grandes: el compilador atrapa erratas en campos que `map[string]any` esconde.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/typed_state
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Usa un checkpointer duradero y registra los tipos de estado personalizados con `checkpoint.Register` para que sobrevivan al round trip en JSON.
2. Pon las invariantes de negocio en `Validate()`: corre antes de cada nodo.
3. Deriva el `thread_id` del usuario autenticado.
4. Prueba la reanudación tras un reinicio del proceso.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
