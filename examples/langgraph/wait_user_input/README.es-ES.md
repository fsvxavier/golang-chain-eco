# wait_user_input

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Pausa un grafo para preguntar algo al usuario y luego reanuda con la respuesta.

## Qué hace

- `ask_location` llama a `graph.Interrupt(ctx, "Where are you?")`; el primer `Invoke` regresa con el interrupt y `GetState` muestra el siguiente nodo.
- `Invoke(ctx, graph.Command{Resume: "Lisbon"})` en el mismo thread continúa y `report` usa la respuesta.

## Cómo aplicarlo

```go
loc := graph.Interrupt(ctx, "Where are you?") // returns the Resume value after the pause
app.Invoke(ctx, graph.Command{Resume: "Lisbon"})
```

No queda nada corriendo mientras espera: el estado está en el checkpointer. Un handler web puede devolver la pregunta al cliente y reanudar cuando llegue la respuesta.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/wait_user_input
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Usa un checkpointer duradero; con `NewMemory` un reinicio pierde la pregunta pendiente.
2. Haz expirar los threads antiguos (`DeleteThread`) y autentica la petición de reanudación contra el dueño del thread.
3. Valida la respuesta antes de reanudar.
4. Prueba la reanudación doble (la misma respuesta enviada dos veces) y la reanudación de un thread desconocido.

## Ver también

- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
