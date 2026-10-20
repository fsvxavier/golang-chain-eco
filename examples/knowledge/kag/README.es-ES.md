# kag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Preguntas multi-hop respondidas recorriendo un grafo de conocimiento en lugar de recuperar el mejor chunk aislado.

## Qué hace

- Tres chunks se convierten en un grafo mediante `kag.Extractor` (entidades y relaciones tipadas; el falso devuelve extracciones estructuradas).
- La pregunta "¿quién prescribió un medicamento que interactúa con la aspirina prescrita a João?" necesita tres saltos entre chunks. La búsqueda vectorial con `K=1` devuelve un chunk y no puede responderla.
- `kag.Engine{Verify: true}` planifica los pasos, recorre las relaciones, verifica la respuesta contra las tripletas usadas y las devuelve como fuentes. Un último caso muestra un grafo cíclico terminando (las aristas visitadas no se repiten).

## Cómo aplicarlo

```go
g := kag.NewMemory()                                   // or integrations/graphstore for Neo4j/Memgraph
(&kag.Extractor{Model: m, Embedder: emb, Graph: g}).Extract(ctx, chunks)
ans, _ := (&kag.Engine{Model: m, Graph: g, Verify: true, MaxHops: 4}).Answer(ctx, knowledge.Question{Text: q})
```

Usa KAG para preguntas sobre relaciones ("quién ... está vinculado a ...", cadenas de dependencias). Los tipos de relación y las etiquetas se validan (`^[A-Z][A-Z0-9_]*$`) antes de llegar a un graph store; nunca concatenes texto del modelo en una consulta.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/kag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo real con tool/function calling (extracción, planificación y verificación son llamadas estructuradas) y un embedder real.
2. Cambia `kag.NewMemory()` por un graph store de `integrations/graphstore` (Neo4j o Memgraph); la suite de conformidad en `internal/conformance` muestra qué debe satisfacer cada store.
3. Audita la calidad de la extracción en una muestra (entidades unidas correctamente, tipos de relación sensatos) antes de confiar en las respuestas; revisa los conteos de `Dropped`.
4. Limita el recorrido (`MaxHops`) y comprueba que las preguntas sin respuesta devuelven "no lo sé".
5. Mantén `Verify` activado para cualquier cosa de cara al usuario.

## Ver también

- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
