# decider_fallback

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Responde las preguntas sobre el historial con `compress.RuleDecider` y muestra cómo `compress.Guarded` vuelve a esas reglas cuando falla el decider del modelo.

## Qué hace

- Construye un historial corto (4 mensajes) y uno largo (120 mensajes, unos 12 mil tokens estimados).
- `compress.Summarize(...).State()` da la vista en objeto JSON que leen los deciders.
- `RuleDecider{}` responde `compress_history`, `level` y `compress_reply` con umbrales fijos (comprimir por encima de 4000 tokens estimados, nivel `full`). No necesita modelo.
- `compress.Guarded` envuelve un `FuncDecider` que siempre devuelve un error. Todas las preguntas pasan a las reglas, así que la ejecución no falla.

## Cómo aplicarlo

```go
st := compress.Summarize(msgs, compress.SummaryOptions{}).State()
g := compress.NewGuarded(modelDecider) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

Usa `Guarded` siempre que un modelo decida algo en la ruta de la petición. Una respuesta lenta, incorrecta o ausente cuesta calidad, nunca la ejecución. La respuesta solo se usa cuando el id y el tipo coinciden y la probabilidad es al menos el umbral.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/decider_fallback
```

La salida es la misma en cada ejecución. El historial corto conserva la transcripción completa, el largo se comprime, y la ejecución protegida imprime las respuestas de las reglas tras el fallo del modelo.

## Probarlo en modo producción

1. Sustituye `modelDecider` por un `compress.HTTPDecider` real (ver `compress/httpdecider.go`) y ajusta el umbral y el timeout a la latencia de tu modelo.
2. Registra qué preguntas cayeron en el fallback. Una tasa creciente indica un decider roto o lento.
3. Mantén `Guarded` alrededor del decider. No llames al decider del modelo directamente.
4. Ejecuta `go test -race -count=1 ./compress` después de cambiar los umbrales.

## Ver también

- [Ejemplo de resumen del historial](../compress_history_summary/README.es-ES.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
