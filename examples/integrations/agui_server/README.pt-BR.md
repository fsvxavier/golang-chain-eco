# agui_server

[English](README.md) | **Português** | [Español](README.es-ES.md)

Sirva um grafo compilado pelo protocolo AG-UI, o stream de eventos que CopilotKit e assistant-ui consomem.

## O que faz

- Um grafo de um nó emite três tokens de modelo com `callbacks.Emit`; `agui.Handler(app, ...)` transforma a execução em server-sent events do AG-UI (`TEXT_MESSAGE_CONTENT` e companhia).
- O exemplo faz POST de uma execução a um servidor `httptest` e imprime o stream de eventos.

## Como aplicar

```go
mux := http.NewServeMux()
mux.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any { return graph.State{"messages": in.Messages} }))
http.ListenAndServe(":8080", mux)
```

Aponte um frontend CopilotKit ou assistant-ui para `/agent`. O segundo argumento mapeia a entrada do frontend para a entrada do seu grafo; num nó real, chame um modelo de chat com streaming para que os tokens cheguem à UI conforme são produzidos.

## Como rodar

```bash
go run ./examples/integrations/agui_server
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Troque os tokens fixos por uma chamada de modelo com streaming e confira se o frontend renderiza os tokens de forma incremental.
2. Coloque autenticação (e CORS, se a UI estiver em outra origem) na frente do handler; derive a thread da sessão, não de ids enviados pelo cliente.
3. Use `http.Server` com timeouts adequados e garanta que o seu proxy não faça buffer de SSE.
4. Adicione um checkpointer durável para threads de vários turnos e teste a reconexão de um cliente.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
