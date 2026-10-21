# compress_prompt_caveman

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Comprime con caveman los turnos antiguos de un historial largo, en la copia que se usa en cada llamada al modelo, antes de cada llamada, usando `middleware.NewCompression`.

## Qué hace

- Un historial de 12 turnos (Human y AI, con un bloque de código Go en cada turno de la AI) entra en `agents.Create`.
- `middleware.NewCompression(decider)` envuelve el decider en `compress.NewGuarded`. El decider de guion responde sí: comprimir los turnos antiguos en `full` y pedir una respuesta concisa.
- Los últimos 4 turnos se mantienen tal cual. Los turnos antiguos pierden muletillas y artículos; las vallas de código, el código en línea, las rutas, las URL y los números siguen idénticos byte a byte.
- Un pequeño middleware `sniff`, colocado después de la compresión, imprime lo que el modelo recibió de verdad.
- Las estimaciones de tokens (`messages.ApproxTokens`) se imprimen antes y después. El historial almacenado nunca se reescribe.

## Cómo aplicarlo

```go
agent, _ := agents.Create(agents.Options{
    Model:      model,
    Middleware: []agents.Middleware{middleware.NewCompression(decider)},
})
```

Ponlo antes del middleware de resumen; define el `MaxTokens` del resumen por encima del umbral del decider, para que actúe solo como respaldo. Usa `compress.RuleDecider{}` cuando no tengas un modelo que decida.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/compress_prompt_caveman
```

Sin clave de API el ejemplo usa un modelo fake de guion, así que funciona sin conexión. Con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` definida, `examples/internal/demo` usa ese proveedor para la respuesta, y la compresión impresa es la misma.

## Cómo probarlo en modo producción

1. Sustituye el decider de guion por `compress.HTTPDecider` o un modelo pequeño; mantén `Floor` en `0.7`, para que una respuesta débil vuelva a las reglas.
2. Comprueba que las llamadas a tools y sus resultados no cambian (no cambian, pero confírmalo con tus propios mensajes de tool).
3. Compara la calidad de las respuestas con y sin compresión en tus conversaciones reales antes de usar `ultra`.
4. Vigila el recuento de tokens en tus trazas; el ahorro depende de cuánto del historial es prosa.

## Ver también

- [Paquete compress](../../../compress/)
- [Ejemplo: compress_shell_rtk](../compress_shell_rtk/README.es-ES.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
