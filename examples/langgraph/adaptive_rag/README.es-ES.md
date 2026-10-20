# adaptive_rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un router decide, por pregunta, si la respuesta sale del vector store o de una búsqueda web.

## Qué hace

- Una arista condicional desde `Start` pide a un modelo router `vectorstore` o `web_search`.
- Cada rama es un nodo que rellena `docs`; ambas siguen a `generate`.
- Ejecuta dos preguntas: una sobre agents de LLM (vector store) y otra sobre el resultado de un partido (web).

## Cómo aplicarlo

```go
g.AddConditionalEdges(graph.Start, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := router.Generate(ctx, []messages.Message{messages.System("Route to vectorstore or web_search."), messages.Human(q)})
    return strings.TrimSpace(out.Text()), nil // the returned string is the next node
})
```

Úsalo cuando tienes una base de conocimiento privada y una fuente abierta. Mantén las opciones del router en un conjunto cerrado y valida su salida; las rutas desconocidas deben caer en una rama por defecto.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/adaptive_rag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Cambia `embeddings.Fake` y el store en memoria por un embedder real y un store de `integrations/vectorstore`; cambia el retriever `web` por un cliente de API de búsqueda real (timeout, tope de resultados).
2. Usa un modelo pequeño y rápido para enrutar y uno más fuerte para la respuesta.
3. Evalúa el router con 30-50 preguntas etiquetadas; sigue la precisión del enrutamiento y el coste de las rutas erróneas.
4. Registra la ruta elegida y las fuentes con cada respuesta.
5. Trata los resultados de la web como texto no confiable (prompt injection) antes de que lleguen a `generate`.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
