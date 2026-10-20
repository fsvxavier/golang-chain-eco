# info_gather

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um chatbot que coleta requisitos ao longo da conversa e depois passa a gerar o prompt final.

## O que faz

- `gather` faz perguntas até o coletor responder `READY: ...`, o que preenche `spec`; então uma aresta condicional vai para `write_prompt`.
- Cada mensagem do usuário é um `Invoke` separado na mesma `thread_id`; um checkpointer mantém a conversa entre os turnos.

## Como aplicar

Use para qualquer coleta guiada: formulários, onboarding, captura de requisitos.

```go
AddConditionalEdges("gather", func(_ context.Context, s graph.State) (any, error) {
    if s["spec"] != nil { return "write_prompt", nil }
    return graph.End, nil // wait for the next user message
})
```

Terminar o grafo em "preciso de mais informação" e retomar com a próxima mensagem na mesma thread é o padrão; não é preciso um processo de longa duração.

## Como rodar

```bash
go run ./examples/langgraph/info_gather
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo real e um checkpointer durável para que a conversa sobreviva a reinícios; derive o `thread_id` da sessão.
2. Torne o sinal de "pronto" estruturado (JSON ou uma chamada de tool) em vez de um prefixo de texto.
3. Valide a spec coletada antes de gerar e limite o número de turnos.
4. Expire threads abandonadas com `saver.DeleteThread`.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
