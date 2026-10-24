# agent_tools

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent que responde desde la base de conocimiento llamando a tools de `knowledge.Toolkit` (`rag_answer`, ...). Los agents corren con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, todo decidido por reglas, sin necesidad de modelo.

## Qué hace

- Dos chunks (una receta y una nota de medicamento) se indexan en un vector store en memoria detrás de un `rag.Pipeline`.
- `knowledge.Toolkit` expone el pipeline y un router como tools (`kb.All()`); el agent guionizado llama a `rag_answer`, recibe la respuesta con citas y responde con `[1]`.

## Cómo aplicarlo

```go
kb := knowledge.Toolkit{RAG: rg, Router: &knowledge.Router{RAG: rg}}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kb.All(), SystemPrompt: "Answer from the knowledge base and cite sources."})
```

Da conocimiento a un agent como tools cuando debe decidir **cuándo** consultar. Añade `CAG` y `KAG` al toolkit y el router elige la técnica por pregunta.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/agent_tools
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa un embedder real y un store duradero, y un modelo real para el pipeline y para el agent.
2. Indexa tu corpus real con `cmd/knowledge index` y comprueba que las respuestas citan las fuentes correctas.
3. Prueba preguntas que el corpus no puede responder: el agent debe decirlo en lugar de adivinar.
4. Mantén los metadatos de fuente (archivo, sección) en cada chunk; las citas valen lo que valgan los metadatos.

## Ver también

- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
