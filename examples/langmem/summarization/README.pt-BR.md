# summarization

[English](README.md) | **Português** | [Español](README.es-ES.md)

Memória de curto prazo: um resumo contínuo mantém uma conversa longa dentro de um orçamento de tokens enquanto o histórico completo permanece no estado.

## O que faz

- `langmem.SummarizationNode` roda antes do modelo. Quando o histórico passa de `MaxTokens` (60 aqui), as mensagens antigas são substituídas na **visão** por um resumo (`MaxSummaryTokens: 15`); o histórico completo continua em `messages`.
- O nó do modelo lê `summarized_messages`. O exemplo imprime o tamanho do histórico e o tamanho da visão do modelo por pergunta, mostrando que a visão continua pequena.

## Como aplicar

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{Model: summarizer, MaxTokens: 2000, MaxSummaryTokens: 300}}
schema := langmem.SummarizationSchema(); schema["messages"] = graph.Messages()
g.AddNode("summarize", node.Node()).AddEdge(graph.Start, "summarize").AddEdge("summarize", "call_model")
```

Use em conversas longas numa única thread. Resuma com um modelo mais barato; mantenha o orçamento bem abaixo da janela do modelo. Em agents, o middleware `Summarization` faz o mesmo dentro de `agents.Create`.

## Como rodar

```bash
go run ./examples/langmem/summarization
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos e orçamentos reais (`MaxTokens` abaixo da janela do modelo menos a resposta e a saída de tools).
2. Use um checkpointer durável: o resumo contínuo vive no estado e precisa sobreviver a reinícios.
3. Teste fatos que existem só em mensagens antigas ("Qual é o meu nome?"): eles precisam sobreviver no resumo.
4. Observe o custo: cada resumo é uma chamada de modelo; resuma em passos maiores em vez de a cada turno.

## Veja também

- [Manual, capítulo 16: Memória de longo prazo (LangMem)](../../../docs/pt-BR/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/pt-BR/LANGMEM.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
