# ralph_mode

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un bucle de agents nuevos sin conversación compartida: el progreso pasa solo por un archivo hasta que la tarea termina.

## Qué hace

- Crea `/progress.md` con tres elementos sin marcar.
- Cada iteración crea un agent **nuevo** y una conversación nueva, lee el archivo, completa exactamente un elemento y lo marca.
- El bucle se detiene cuando el agent responde `ALL DONE`; el ejemplo imprime el número de elementos marcados por iteración.

## Cómo aplicarlo

Úsalo para trabajo demasiado grande para una sola ventana de contexto: cada pasada empieza limpia, así que el contexto nunca se desborda, y el archivo es la fuente de la verdad.

```go
for i := 0; i < maxIterations; i++ {
    agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: be})
    out, _ := agent.Invoke(ctx, graph.State{"messages": "Read /progress.md and complete exactly one unchecked item."})
    if strings.Contains(lastText(out), "ALL DONE") { break }
}
```

Escribe el archivo de progreso de modo que cualquier agent nuevo pueda retomar: un elemento por línea, el estado y un puntero al artefacto.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/ralph_mode
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa un modelo real y un directorio `backends.Filesystem` real, bajo control de versiones.
2. Acota siempre las iteraciones (`maxIterations`) y añade un timeout de reloj; un agent que nunca marca un elemento como hecho se quedaría en bucle para siempre.
3. Verifica "hecho" fuera del modelo: ejecuta las pruebas o un checker tras cada iteración y revierte el elemento si falla.
4. Haz commit en cada iteración para que una mala pasada se deshaga con un único revert.
5. Sigue los tokens por iteración; si el coste crece, el archivo de progreso es demasiado grande o vago.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
