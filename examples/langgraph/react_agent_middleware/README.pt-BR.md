# react_agent_middleware

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent com chamadas de tool montado com `agents.Create`, um middleware e updates em streaming.

## O que faz

- Uma tool (`get_weather`) e `middleware.ModelCallLimit{RunLimit: 5}`.
- `StreamEvents` com `graph.ModeUpdates` imprime o update de cada nó conforme acontece (chamada do modelo, chamada de tool, resposta final).

## Como aplicar

```go
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{weather}, SystemPrompt: "...",
    Middleware: []agents.Middleware{middleware.ModelCallLimit{RunLimit: 5}},
})
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": q}, graph.ModeUpdates) { ... }
```

Este é o ponto de partida padrão de um agent. Faça streaming para a UI para que os usuários vejam o progresso; o schema da tool vem da struct de argumentos em Go (tags `json` e `description`).

## Como rodar

```bash
go run ./examples/langgraph/react_agent_middleware
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Rode com uma chave real e uma API de clima real atrás da tool (timeout, mapeamento de erros).
2. Adicione `ToolRetry`, `ModelRetry` e `ModelFallback`; veja `langchain/middleware_stack`.
3. Adicione um checkpointer e um `thread_id` se a conversa continuar entre requisições.
4. Confira o schema de tool que o modelo recebe; tags `description` vagas causam argumentos errados.

## Veja também

- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Manual, capítulo 5: Tools](../../../docs/pt-BR/manual/05-tools.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
