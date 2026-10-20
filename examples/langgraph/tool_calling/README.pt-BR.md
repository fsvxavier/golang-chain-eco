# tool_calling

[English](README.md) | **Português** | [Español](README.es-ES.md)

Vincule tools a um modelo, inspecione as chamadas de tool que ele faz e rode-as de forma concorrente com `ToolNode`.

## O que faz

- Três tools (`add`, `multiply`, `divide`) vinculadas com `models.BindTools`.
- O modelo roteirizado pede quatro chamadas de tool de uma vez, incluindo `divide` por zero e uma tool desconhecida (`sqrt`).
- `prebuilt.NewToolNode` as roda de forma concorrente; o exemplo imprime cada resultado e status, mostrando que erros e tools desconhecidas voltam como mensagens de erro em vez de derrubar a execução.

## Como aplicar

```go
model := models.BindTools(chatModel, tools...)
ai, _ := model.Generate(ctx, msgs)                // ai.ToolCalls
out, _ := prebuilt.NewToolNode(tools...).Node()(ctx, graph.State{"messages": []messages.Message{ai}})
```

Devolva `tools.Errorf(...)` para falhas esperadas, para que o modelo possa se recuperar. Os ids de chamadas de tool precisam ser únicos dentro de uma conversa.

## Como rodar

```bash
go run ./examples/langgraph/tool_calling
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo real e confira se ele produz argumentos válidos para os seus schemas; melhore as tags `description` onde não produzir.
2. Defina um timeout por tool e um limite de concorrência se as tools acessam APIs com rate limit.
3. Nunca coloque segredos nos resultados das tools; eles voltam ao modelo e vão para os traces.
4. Teste os caminhos de tool desconhecida e de argumentos inválidos; eles acontecem em produção.

## Veja também

- [Manual, capítulo 5: Tools](../../../docs/pt-BR/manual/05-tools.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
