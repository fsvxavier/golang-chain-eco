# graph_rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Recuperación que sigue enlaces: el mejor resultado trae páginas relacionadas que una búsqueda por similitud simple pasaría por alto.

## Qué hace

- Cinco páginas con metadatos `links` forman un pequeño grafo (intro → channels, scheduler; channels → select; más una página de CSS sin relación).
- La similitud simple top-2 devuelve la intro y un vecino; `graphretriever.Retriever` (inicio 1, profundidad 2, arista `links` → `id`) recorre los enlaces y devuelve las páginas conectadas.

## Cómo aplicarlo

```go
&graphretriever.Retriever{
    Store: vs, K: 6, StartK: 1, Depth: 2,
    Edges: []graphretriever.Edge{{From: "links", To: "id"}},
}
```

Úsalo en documentación, wikis y bases de código donde el contexto relevante está enlazado, no es similar. Guarda los destinos de los enlaces en los metadatos de los documentos al indexar. Para un grafo de conocimiento completo con razonamiento multi-hop, mira `examples/knowledge/kag`.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/graph_rag
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Indexa documentos reales con un embedder real en un store duradero (`integrations/vectorstore`), extrayendo los enlaces de la fuente (hrefs, imports, enlaces de wiki).
2. Ajusta `StartK`, `Depth` y `K` con preguntas reales; un recorrido más profundo suma recall y ruido.
3. Limita el total de documentos devueltos para proteger el presupuesto de contexto del modelo.
4. Compara la calidad de las respuestas con la similitud simple en 20 preguntas antes de adoptarlo.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
