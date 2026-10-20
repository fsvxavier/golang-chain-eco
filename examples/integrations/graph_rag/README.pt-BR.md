# graph_rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Recuperação que segue links: o melhor resultado traz páginas relacionadas que uma busca por similaridade simples perderia.

## O que faz

- Cinco páginas com metadados `links` formam um pequeno grafo (intro → channels, scheduler; channels → select; mais uma página de CSS sem relação).
- A similaridade simples top-2 devolve a intro e um vizinho; `graphretriever.Retriever` (início 1, profundidade 2, aresta `links` → `id`) percorre os links e devolve as páginas conectadas.

## Como aplicar

```go
&graphretriever.Retriever{
    Store: vs, K: 6, StartK: 1, Depth: 2,
    Edges: []graphretriever.Edge{{From: "links", To: "id"}},
}
```

Use em documentação, wikis e bases de código em que o contexto relevante é ligado, não similar. Guarde os destinos dos links nos metadados dos documentos na hora de indexar. Para um grafo de conhecimento completo com raciocínio multi-hop, veja `examples/knowledge/kag`.

## Como rodar

```bash
go run ./examples/integrations/graph_rag
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Indexe documentos reais com um embedder real num store durável (`integrations/vectorstore`), extraindo os links da fonte (hrefs, imports, links de wiki).
2. Ajuste `StartK`, `Depth` e `K` com perguntas reais; uma travessia mais profunda soma recall e ruído.
3. Limite o total de documentos devolvidos para proteger o orçamento de contexto do modelo.
4. Compare a qualidade das respostas com a similaridade simples em 20 perguntas antes de adotar.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores e retrievers](../../../docs/pt-BR/manual/06-rag.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
