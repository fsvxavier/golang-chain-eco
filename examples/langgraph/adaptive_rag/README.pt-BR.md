# adaptive_rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um roteador decide, por pergunta, se a resposta vem do vector store ou de uma busca na web.

## O que faz

- Uma aresta condicional a partir de `Start` pede a um modelo roteador `vectorstore` ou `web_search`.
- Cada ramo é um nó que preenche `docs`; ambos seguem para `generate`.
- Roda duas perguntas: uma sobre agents de LLM (vector store) e outra sobre o resultado de uma partida (web).

## Como aplicar

```go
g.AddConditionalEdges(graph.Start, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := router.Generate(ctx, []messages.Message{messages.System("Route to vectorstore or web_search."), messages.Human(q)})
    return strings.TrimSpace(out.Text()), nil // the returned string is the next node
})
```

Use quando você tem uma base de conhecimento privada e uma fonte aberta. Mantenha as escolhas do roteador num conjunto fechado e valide a saída dele; rotas desconhecidas devem cair num ramo padrão.

## Como rodar

```bash
go run ./examples/langgraph/adaptive_rag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Troque `embeddings.Fake` e o store em memória por um embedder real e um store de `integrations/vectorstore`; troque o retriever `web` por um cliente de API de busca real (timeout, limite de resultados).
2. Use um modelo pequeno e rápido para rotear e um mais forte para a resposta.
3. Avalie o roteador com 30-50 perguntas rotuladas; acompanhe a acurácia do roteamento e o custo das rotas erradas.
4. Registre a rota escolhida e as fontes junto com cada resposta.
5. Trate os resultados da web como texto não confiável (prompt injection) antes de chegarem a `generate`.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
