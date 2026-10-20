# middleware_stack

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent envuelto en los middlewares que quieres en producción: redacción de PII, retry, fallback de modelo y límites de llamadas.

## Qué hace

- El modelo primario siempre falla con 503; el modelo de respaldo funciona.
- `PII` oculta el correo antes de que cualquier modelo lo vea. `ModelRetry` reintenta el primario dos veces y `ModelFallback` cambia al respaldo.
- `ModelCallLimit` y `ToolCallLimit` limitan cuántas llamadas puede hacer una ejecución.
- Imprime el mensaje almacenado del usuario, el número de intentos en el primario y la respuesta del respaldo, lo que confirma que el correo llegó redactado.

## Cómo aplicarlo

Compón `agents.Middleware` en orden; el primero es el más externo.

```go
agents.Create(agents.Options{
    Model: primary, Tools: tools,
    Middleware: []agents.Middleware{
        middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail}},
        middleware.ModelRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
        middleware.ModelFallback{Fallbacks: []models.ChatModel{backup}},
        middleware.ModelCallLimit{RunLimit: 25},
        middleware.ToolCallLimit{RunLimit: 10},
    },
})
```

El orden importa: oculta primero para que los reintentos y los fallbacks nunca vean PII en bruto, reintenta antes de caer al fallback y deja los límites al final para que cuenten llamadas reales.

## Cómo ejecutarlo

```bash
go run ./examples/langchain/middleware_stack
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Cambia los dos falsos por modelos reales de dos proveedores (por ejemplo Anthropic como primario y OpenAI como fallback).
2. Fuerza un fallo (clave inválida o `httptest` devolviendo 503 en el primario) y confirma que el fallback responde y que el log muestra los reintentos.
3. Envía PII realista (correos, números de tarjeta) e inspecciona la petición que llega a cada proveedor para confirmar que está redactada.
4. Define `RunLimit` a partir de un p99 medido de llamadas por tarea, no de una suposición, y alerta cuando una ejecución lo alcance.
5. Añade `Summarization` para threads largos y un `RecursionLimit` explícito en `runnables.Config` (ver el checklist).

## Ver también

- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Manual, capítulo 14: Producción, seguridad y troubleshooting](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
