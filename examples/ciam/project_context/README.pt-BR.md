# project_context

[English](README.md) | **Português** | [Español](README.es-ES.md)

Contexto barato e preciso de projeto para um assistente de IA: índice incremental, busca híbrida, compressão, decisões (ADR/PRD/Plano), memória e um servidor MCP.

## O que faz

- Indexa um pequeno projeto estilo Django mais um ADR, um PRD e um plano, de forma incremental (a segunda execução não gera embeddings de nada; editar um arquivo gera embeddings de um).
- Busca híbrida (vetor + BM25, ranqueada com RRF) filtrada por tipo de chunk, compressão que mantém assinaturas e descarta documentação, `DecisionContext` (código mais os ADRs/PRDs por trás dele), `Remember`/`Recall`, substituição de ADR e as mesmas capacidades servidas como servidor MCP.
- Imprime a estimativa de tokens economizados em relação a ler os arquivos.

## Como aplicar

```go
m := ciam.New(embedder, nil)
m.Index(ctx, root, ciam.IndexOptions{})
res, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "send receipt", Types: []string{"task"}, Limit: 3})
dc, _ := m.DecisionContext(ctx, "shop", "invoice total cents stored", 3)
srv := ciam.Toolkit{M: m, Project: root}.MCPServer(nil) // for VS Code / Cursor / Claude
```

Ou use a CLI: `go run ./cmd/ciam index|search|decision|mcp`. Escreva o PORQUÊ como ADRs para que o próximo assistente (ou pessoa) o encontre.

## Como rodar

```bash
go run ./examples/ciam/project_context
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Troque `embeddings.Fake{}` por um embedder real (o comentário aponta para `providers/ollama.Embeddings`) e reindexe; confira a qualidade da busca com 20 perguntas reais sobre o seu código.
2. Indexe o seu repositório real e mantenha o índice fora do repo; rode `Index` de novo no CI ou num hook do git (é incremental).
3. Registre o servidor MCP no seu editor (`go run ./cmd/ciam mcp`) e confirme que o assistente chama tools `ciam_*` em vez de ler arquivos inteiros.
4. Revise documentos ADR/PRD como código; ADRs são substituídos, nunca editados.
5. Acompanhe `Status` (buscas, cache hits, tokens economizados) para confirmar que compensa.

## Veja também

- [Manual, capítulo 18: ciam: contexto de projeto barato e preciso para assistentes](../../../docs/pt-BR/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/pt-BR/CIAM.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
