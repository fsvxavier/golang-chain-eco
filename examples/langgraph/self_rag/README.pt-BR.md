# self_rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Recupera, gera e depois avalia se a resposta é fundamentada, regenerando quando os documentos não a sustentam.

## O que faz

- `retrieve` e `generate` rodam uma vez; uma aresta condicional pergunta a um modelo juiz se a resposta é fundamentada nos documentos.
- A primeira resposta roteirizada é uma alucinação ("mais rápido que a luz"), o juiz diz não, `generate` roda de novo (`graph.Sum()` limita as tentativas a 3) e a resposta fundamentada passa.

## Como aplicar

```go
AddConditionalEdges("generate", func(ctx context.Context, s graph.State) (any, error) {
    if !grounded(ctx, s) && s["tries"].(int) < 3 { return "generate", nil }
    return graph.End, nil
})
```

Adicione esta checagem quando respostas erradas saem caro. Uma versão mais completa também avalia a relevância dos documentos (veja `corrective_rag`) e reescreve a consulta quando a própria recuperação falhou.

## Como rodar

```bash
go run ./examples/langgraph/self_rag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um embedder, um vector store e modelos reais; use um modelo diferente como juiz quando possível.
2. Avalie o juiz em respostas que você rotulou como fundamentadas ou não; acompanhe os falsos positivos (alucinações que passaram).
3. Decida o que acontece depois da última tentativa: responder com ressalva, devolver "não sei" ou escalar.
4. Registre os resultados de fundamentação; uma taxa de fundamentadas em queda sinaliza deriva da recuperação.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
