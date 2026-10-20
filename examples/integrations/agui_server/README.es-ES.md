# agui_server

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Sirve un grafo compilado con el protocolo AG-UI, el stream de eventos que consumen CopilotKit y assistant-ui.

## Qué hace

- Un grafo de un nodo emite tres tokens de modelo con `callbacks.Emit`; `agui.Handler(app, ...)` convierte la ejecución en server-sent events de AG-UI (`TEXT_MESSAGE_CONTENT` y compañía).
- El ejemplo hace POST de una ejecución a un servidor `httptest` e imprime el stream de eventos.

## Cómo aplicarlo

```go
mux := http.NewServeMux()
mux.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any { return graph.State{"messages": in.Messages} }))
http.ListenAndServe(":8080", mux)
```

Apunta un frontend CopilotKit o assistant-ui a `/agent`. El segundo argumento mapea la entrada del frontend a la entrada de tu grafo; en un nodo real, llama a un modelo de chat con streaming para que los tokens lleguen a la UI a medida que se producen.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/agui_server
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Cambia los tokens fijos por una llamada a un modelo con streaming y comprueba que el frontend renderiza los tokens de forma incremental.
2. Pon autenticación (y CORS, si la UI está en otro origen) delante del handler; deriva el thread de la sesión, no de ids enviados por el cliente.
3. Usa `http.Server` con timeouts adecuados y asegúrate de que tu proxy no haga buffer de SSE.
4. Añade un checkpointer duradero para threads de varios turnos y prueba la reconexión de un cliente.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
