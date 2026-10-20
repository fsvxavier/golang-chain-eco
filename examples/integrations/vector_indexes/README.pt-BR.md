# vector_indexes

[English](README.md) | **Português** | [Español](README.es-ES.md)

Os dois índices vetoriais em processo, exato (flat) e aproximado (floresta estilo Annoy), com o recall medido contra a resposta exata.

## O que faz

- Monta 5.000 vetores aleatórios de 32 dimensões em `vectorstore.NewFlat()` e `vectorstore.Annoy{Trees: 20}`.
- Mede o recall@10 da floresta contra o índice exato no `SearchK` padrão e em 1500 (mais candidatos, melhor recall, consultas mais lentas), depois salva e recarrega o índice flat e confere que o mesmo vizinho mais próximo volta.

## Como aplicar

```go
flat := vectorstore.NewFlat()          // exact, simplest
ann  := &vectorstore.Annoy{Trees: 20}  // approximate, faster on large sets
flat.Upsert(ctx, records); hits, _ := flat.Query(ctx, vec, 10)
flat.Save(path); flat, _ = vectorstore.LoadFlat(path)
```

Comece com `Flat`; passe à floresta quando a busca exata ficar lenta demais. Meça sempre o recall com **os seus** vetores: vetores gaussianos aleatórios, como aqui, são um caso difícil.

## Como rodar

```bash
go run ./examples/integrations/vector_indexes
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Use os seus embeddings reais e uma amostra representativa de consultas, e escolha `Trees` e `SearchK` para o recall que você precisa (por exemplo 95%) com uma latência aceitável.
2. Reconstrua ou recarregue o índice na inicialização a partir de um arquivo persistido; mantenha-o consistente com a fonte da verdade.
3. Para corpora muito grandes ou compartilhados, use um store em rede de `integrations/vectorstore` em vez de um índice em processo.
4. Meça o recall de novo depois de trocar o modelo de embedding.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
