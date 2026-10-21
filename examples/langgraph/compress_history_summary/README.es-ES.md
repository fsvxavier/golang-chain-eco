# compress_history_summary

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Construye un historial largo (40 turnos con resultados de tools, unos 13 mil tokens estimados) y muestra el `compress.Summary` acotado que leen los deciders en lugar de la transcripción.

## Qué hace

- `compress.Summarize(msgs, compress.SummaryOptions{})` cuenta mensajes, turnos, tokens, el resultado de tool más grande y la fracción de bytes de tools, detecta el idioma y conserva los últimos 3 mensajes recortados a 160 bytes cada uno.
- `Summary.EncodedTokens()` mide el JSON de `Summary.State()` a 4 bytes por token. Debe quedarse en 700 tokens o menos (el presupuesto). El ejemplo termina con error si no lo cumple.
- El ejemplo imprime el número de mensajes, los tokens del historial, los tokens del resumen y el JSON del resumen.

## Cómo aplicarlo

```go
s := compress.Summarize(msgs, compress.SummaryOptions{})
answers, err := compress.NewGuarded(modelDecider).Decide(ctx, s.State(), compress.HistoryQuestions())
```

Envía el Summary al decider, no el historial. Su tamaño no crece con el historial: un mensaje de 5 MB cuesta lo mismo que uno corto, porque solo se lee un prefijo acotado de cada mensaje.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/compress_history_summary
```

Líneas de salida esperadas: `messages: 120`, `history tokens: 13160` (o cercano), `summary tokens: 130 (budget 700)`, y luego el JSON `summary:` con `messages`, `turns`, `tokens`, `tool_share`, `largest_tool_bytes`, `code_share`, `json_tool_bytes`, `lang` y `tail`.

## Probarlo en modo producción

1. Usa una transcripción real y comprueba que `summary tokens` se mantiene bajo el presupuesto en las sesiones más grandes.
2. Revisa `lang` en historiales con varios idiomas. El detector solo muestrea 4 KiB por mensaje.
3. Mantén corto el `tail` del resumen. Lleva los turnos más recientes, así que un tail incorrecto es una decisión incorrecta.
4. Ejecuta `go test -race -count=1 ./compress` después de cambiar el presupuesto o las opciones.

## Ver también

- [Ejemplo de fallback de decider](../decider_fallback/README.es-ES.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
