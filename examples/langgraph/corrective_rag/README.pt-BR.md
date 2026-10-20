# corrective_rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Recupera, avalia cada documento, recorre a uma busca na web quando nada é relevante e então responde.

## O que faz

- `retrieve` busca os melhores documentos; `grade` pergunta a um modelo avaliador "relevante? yes/no" por documento e mantém só os "yes".
- Se nenhum sobrar, `web_search` fornece um substituto; do contrário `generate` responde com os documentos mantidos.
- Duas perguntas mostram os dois caminhos.

## Como aplicar

```go
AddConditionalEdges("grade", func(_ context.Context, s graph.State) (any, error) {
    if s["web"] == true { return "web_search", nil }
    return "generate", nil
})
```

Use quando a qualidade da recuperação varia e uma resposta errada com confiança custa mais do que uma chamada extra de modelo por documento. Avalie em paralelo se a latência importar.

## Como rodar

```bash
go run ./examples/langgraph/corrective_rag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um embedder e um vector store reais, modelos reais de avaliação e resposta e uma tool de busca real para o fallback.
2. Meça o avaliador: precisão e recall em documentos que você rotulou à mão; um avaliador que rejeita tudo manda toda consulta para a web.
3. Limite o número de documentos avaliados por consulta para controlar custo e latência.
4. Registre com que frequência o fallback dispara; uma taxa alta indica que a recuperação ou o chunking precisa de trabalho, não um avaliador melhor.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
