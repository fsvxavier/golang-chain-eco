# summarization

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Memoria de corto plazo: un resumen continuo mantiene una conversación larga dentro de un presupuesto de tokens mientras el historial completo permanece en el estado.

## Qué hace

- `langmem.SummarizationNode` corre antes del modelo. Cuando el historial supera `MaxTokens` (60 aquí), los mensajes antiguos se sustituyen en la **vista** por un resumen (`MaxSummaryTokens: 15`); el historial completo sigue en `messages`.
- El nodo del modelo lee `summarized_messages`. El ejemplo imprime el tamaño del historial y el tamaño de la vista del modelo por pregunta, mostrando que la vista se mantiene pequeña.

## Cómo aplicarlo

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{Model: summarizer, MaxTokens: 2000, MaxSummaryTokens: 300}}
schema := langmem.SummarizationSchema(); schema["messages"] = graph.Messages()
g.AddNode("summarize", node.Node()).AddEdge(graph.Start, "summarize").AddEdge("summarize", "call_model")
```

Úsalo en conversaciones largas en un único thread. Resume con un modelo más barato; mantén el presupuesto bien por debajo de la ventana del modelo. En agents, el middleware `Summarization` hace lo mismo dentro de `agents.Create`.

## Cómo ejecutarlo

```bash
go run ./examples/langmem/summarization
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos y presupuestos reales (`MaxTokens` por debajo de la ventana del modelo menos la respuesta y la salida de tools).
2. Usa un checkpointer duradero: el resumen continuo vive en el estado y debe sobrevivir a los reinicios.
3. Prueba hechos que solo están en mensajes antiguos ("¿Cómo me llamo?"): deben sobrevivir en el resumen.
4. Vigila el coste: cada resumen es una llamada a un modelo; resume en pasos mayores en lugar de en cada turno.

## Ver también

- [Manual, capítulo 16: Memoria a largo plazo (LangMem)](../../../docs/es-ES/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/es-ES/LANGMEM.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
