# cag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Generación aumentada por caché: todo el corpus va en un prefijo de system prompt estable byte a byte, que el proveedor cachea entre llamadas.

## Qué hace

- `cag.NewCorpus` calcula el hash de los documentos y estima los tokens; `cag.Engine` los pone en el system prompt.
- La pregunta 1 reporta `CacheCreationTokens`, la pregunta 2 reporta `CacheReadTokens` (el paso de trace `cache_hit`), como lo hace un proveedor con caché de prefijo.
- Con una `Window` deliberadamente pequeña, el engine no puede encajar el corpus y recurre a un respondedor por palabras clave (el paso de trace `fallback`).

## Cómo aplicarlo

```go
corp := cag.NewCorpus(docs)
engine := &cag.Engine{Model: model, Corpus: corp, Fallback: ragOrKeywordAnswerer}
ans, _ := engine.Answer(ctx, knowledge.Question{Text: q})
```

Usa CAG cuando el corpus es pequeño (debe caber en la ventana de contexto), estable y se consulta a menudo: no hay paso de recuperación y los tokens de entrada cacheados cuestan mucho menos. Mantén el prefijo idéntico byte a byte entre llamadas (mismo orden, mismo texto); el engine lo marca con `cache_control` para Anthropic.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/cag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa Anthropic (`ANTHROPIC_API_KEY`) con un corpus real; haz las mismas dos preguntas dos veces y confirma que `cache_read` es distinto de cero en la segunda llamada (ese es el ahorro).
2. Ojo con la vida útil de la caché: un prefijo frío tras el TTL paga el precio completo. Mide el coste por pregunta con un patrón de tráfico realista.
3. Reconstruye el corpus (nuevo hash) solo cuando cambien los documentos; un byte distinto invalida la caché.
4. Define `Window`/`Reserve` para el modelo que usas y proporciona un `Fallback` real (normalmente el pipeline RAG) para cuando el corpus supere la ventana.

## Ver también

- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
