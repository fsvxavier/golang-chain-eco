# chain

[English](README.md) | [Português](README.pt-BR.md) | **Español**

La chain más pequeña posible: un prompt de chat, un modelo y un parser de salida encadenados (el equivalente de LCEL).

## Qué hace

- Construye un prompt de chat de dos mensajes (`system` + `human`) con la variable `{topic}`.
- Encadena `prompt | modelo | parser` con `runnables.Pipe3`, tipado de punta a punta: `prompts.Values` de entrada, `string` de salida.
- Invoca una vez con `topic = "goroutines"` e imprime la respuesta de una frase.

## Cómo aplicarlo

Usa este formato para cualquier llamada a un LLM de un paso que deba ser testeable y tipada.

```go
prompt := prompts.Chat([2]string{"system", "..."}, [2]string{"human", "Explain {topic}."})
chain  := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
    prompt, models.AsRunnable(model), outputparsers.Str{})
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

- Cambia `outputparsers.Str{}` por un parser JSON o estructurado cuando quien llama necesite datos, no texto.
- Como cada etapa es un `Runnable`, puedes añadir `WithRetry`, callbacks o un tracer a toda la chain sin tocar las etapas.

## Cómo ejecutarlo

```bash
go run ./examples/langchain/chain
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Exporta una clave de proveedor y ejecuta el ejemplo; la respuesta debe ser una frase sobre goroutines.
2. Envuelve la llamada en `context.WithTimeout` y añade `runnables.RetryOptions` para que un proveedor lento o con rate limit no bloquee a quien llama.
3. Conecta un tracer (LangSmith, Langfuse u OTLP; ver el capítulo 12) y confirma un trace por `Invoke` con el uso de tokens.
4. Pon `models.NewRateLimiter` delante del proveedor si varias peticiones comparten la misma clave.
5. Trata el texto del prompt en code review como cualquier otro código: es comportamiento.

## Ver también

- [Manual, capítulo 4: Prompts, chains y parsers](../../../docs/es-ES/manual/04-chains-prompts-parsers.md)
- [Manual, capítulo 3: Modelos y proveedores](../../../docs/es-ES/manual/03-modelos-e-provedores.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
