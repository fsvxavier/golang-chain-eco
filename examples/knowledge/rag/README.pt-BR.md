# rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Respostas com recuperação e citações, busca híbrida (vetor + BM25) e indexação incremental.

## O que faz

- Indexa cinco documentos curtos (em português, sobre um roteador fictício) com `rag.NewIndexer`.
- Compara a busca vetorial simples com a híbrida (`pipe.Hybrid = true`): o código de erro exato `KRX-9921` é encontrado pelo BM25 mesmo quando os embeddings o borram.
- Responde com citações `[n]` e imprime os passos de `Trace` e as fontes.
- Reindexa: um corpus inalterado é ignorado por completo (`Added=0 Skipped=5`), e editar um documento remove e readiciona só os chunks dele.

## Como aplicar

```go
indexer := rag.NewIndexer(store, nil)
stats, _ := indexer.Index(ctx, loader)          // incremental: Added / Skipped / Removed
pipe := &rag.Pipeline{Store: store, Model: model, K: 3, Hybrid: true, Docs: indexer.Documents()}
ans, _ := pipe.Answer(ctx, knowledge.Question{Text: q}) // ans.Text, ans.Sources, ans.Trace
```

Use RAG quando o corpus é grande ou muda com frequência. Ative `Hybrid` quando os usuários buscam por códigos, nomes ou identificadores. A CLI faz o mesmo sobre arquivos: `go run ./cmd/knowledge <index|ask|mcp|serve>`.

## Como rodar

```bash
go run ./examples/knowledge/rag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um embedder real e um vector store durável (`integrations/vectorstore`), e um modelo real; mantenha metadados de fonte (`source`) em todo documento para que as citações façam sentido.
2. Indexe com a CLI ou com o seu loader num agendamento; a segunda execução deve reportar principalmente `Skipped` (é isso que torna a reindexação barata).
3. Monte 20-30 perguntas com respostas conhecidas, incluindo códigos/identificadores e perguntas sem resposta; verifique a recuperação (o chunk certo entre os K primeiros) separadamente da geração.
4. Mantenha o comportamento "não está no contexto": o modelo deve dizer que não sabe.
5. Registre `Trace` e as fontes por resposta para depuração e auditoria.

## Veja também

- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
