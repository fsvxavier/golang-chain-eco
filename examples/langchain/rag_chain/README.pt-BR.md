# rag_chain

[English](README.md) | **Português** | [Español](README.es-ES.md)

Uma chain de recuperação clássica montada com runnables: dividir, gerar embeddings, recuperar, montar o prompt e responder.

## O que faz

- Divide um corpus pequeno com `textsplitters.NewRecursive`, gera embeddings dos trechos num vector store em memória e monta um retriever com `K: 2`.
- `runnables.Assign` adiciona um campo `context` (os trechos recuperados) ao mapa `{"question": ...}`; então prompt, modelo e parser fecham a chain.
- Pergunta "What are goroutines?" e imprime uma resposta fundamentada no texto recuperado.

## Como aplicar

Use quando o conhecimento cabe num retriever simples e você quer o fluxo inteiro como um único `Runnable` componível.

```go
store, _ := vectorstores.FromDocuments(ctx, embedder, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
withCtx := runnables.Assign(map[string]runnables.Runnable[map[string]any, any]{"context": retrieve})
```

Para indexação incremental, busca híbrida ou um roteador entre RAG, CAG e KAG, use os pacotes `rag` e `knowledge` (capítulo 20).

## Como rodar

```bash
go run ./examples/langchain/rag_chain
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Troque `embeddings.Fake` por um embedder real (por exemplo `providers/ollama.Embeddings` ou um compatível com OpenAI) e `FromDocuments` por um vector store durável de `integrations/vectorstore`.
2. Indexe seus documentos reais uma vez, fora do caminho da requisição, e mantenha o tamanho do chunk e a sobreposição sob controle de versão.
3. Meça a recuperação antes da geração: faça 20 perguntas com respostas conhecidas e confira se o chunk certo está entre os `K` primeiros.
4. Mantenha a instrução "responda só com este contexto" e teste uma pergunta que o corpus não responde: o modelo deve dizer que não sabe.
5. Registre os chunks recuperados junto com cada resposta para rastrear uma resposta errada até a recuperação ou a geração.

## Veja também

- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
