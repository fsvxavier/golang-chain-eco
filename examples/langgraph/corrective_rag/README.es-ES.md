# corrective_rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Recupera, evalúa cada documento, recurre a una búsqueda web cuando nada es relevante y luego responde.

## Qué hace

- `retrieve` trae los mejores documentos; `grade` pregunta a un modelo evaluador "¿relevante? yes/no" por documento y conserva solo los "yes".
- Si no sobrevive ninguno, `web_search` aporta un sustituto; si no, `generate` responde con los documentos conservados.
- Dos preguntas muestran los dos caminos.

## Cómo aplicarlo

```go
AddConditionalEdges("grade", func(_ context.Context, s graph.State) (any, error) {
    if s["web"] == true { return "web_search", nil }
    return "generate", nil
})
```

Úsalo cuando la calidad de la recuperación varía y una respuesta errónea con seguridad cuesta más que una llamada extra al modelo por documento. Evalúa en paralelo si la latencia importa.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/corrective_rag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un embedder y un vector store reales, modelos reales de evaluación y respuesta y una tool de búsqueda real para el fallback.
2. Mide el evaluador: precisión y recall sobre documentos que etiquetaste a mano; un evaluador que rechaza todo manda cada consulta a la web.
3. Limita el número de documentos evaluados por consulta para acotar coste y latencia.
4. Registra con qué frecuencia se dispara el fallback; una tasa alta indica que la recuperación o el chunking necesita trabajo, no un mejor evaluador.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
