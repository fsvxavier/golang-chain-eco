# router

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un router delante de RAG, CAG y KAG que elige la técnica correcta por pregunta, con rutas forzadas y fallback.

## Qué hace

- Los mismos tres chunks alimentan un vector store (RAG), un corpus en caché (CAG) y un grafo extraído (KAG).
- Cinco escenarios: una pregunta multi-hop va al grafo; una pregunta simple sobre un corpus pequeño y estable va a CAG; la misma pregunta con un corpus volátil va a RAG; `Force: "kag"` anula la elección; y cuando KAG falla, `Fallback: true` hace que RAG responda. Los pasos de trace `route` y `fallback` muestran el porqué.

## Cómo aplicarlo

```go
r := &knowledge.Router{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine,
    Profile:  knowledge.Profile{HasGraph: true, CorpusTokens: n, ContextWindow: 200000, Volatile: false},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: q})
```

Describe tus datos con honestidad en `Profile` (tamaño, ventana, volatilidad, existencia de grafo): eso es lo que leen las reglas de enrutamiento. Expón el router a agents como tools o MCP con `knowledge.Toolkit`.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/router
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos reales, un vector store y un graph store duraderos y define `Profile` a partir de mediciones (tokens del corpus, con qué frecuencia cambian los documentos).
2. Arma un conjunto etiquetado de preguntas por técnica y comprueba que la ruta elegida coincide; revisa los enrutamientos erróneos.
3. Mantén `Fallback` activado y pruébalo rompiendo el graph store (la respuesta debe seguir llegando vía RAG).
4. Registra el trace `route` por petición y sigue la distribución; un cambio sorprendente indica que el perfil de tus datos cambió.

## Ver también

- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
