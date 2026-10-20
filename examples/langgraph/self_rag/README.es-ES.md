# self_rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Recupera, genera y luego evalúa si la respuesta está fundamentada, regenerando cuando los documentos no la sustentan.

## Qué hace

- `retrieve` y `generate` corren una vez; una arista condicional pregunta a un modelo juez si la respuesta está fundamentada en los documentos.
- La primera respuesta guionizada es una alucinación ("más rápido que la luz"), el juez dice que no, `generate` corre de nuevo (`graph.Sum()` limita los intentos a 3) y la respuesta fundamentada pasa.

## Cómo aplicarlo

```go
AddConditionalEdges("generate", func(ctx context.Context, s graph.State) (any, error) {
    if !grounded(ctx, s) && s["tries"].(int) < 3 { return "generate", nil }
    return graph.End, nil
})
```

Añade esta comprobación cuando las respuestas erróneas salen caras. Una versión más completa también evalúa la relevancia de los documentos (ver `corrective_rag`) y reescribe la consulta cuando la propia recuperación falló.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/self_rag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un embedder, un vector store y modelos reales; usa un modelo distinto como juez cuando sea posible.
2. Evalúa al juez con respuestas que etiquetaste como fundamentadas o no; sigue los falsos positivos (alucinaciones que pasaron).
3. Decide qué ocurre tras el último intento: responder con salvedad, devolver "no lo sé" o escalar.
4. Registra los resultados de fundamentación; una tasa de fundamentadas en descenso señala deriva de la recuperación.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
