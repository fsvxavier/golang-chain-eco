# structured_output

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent que termina con una respuesta tipada y validada en lugar de texto libre. El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, y los resultados JSON de tools se recodifican en TOON, todo decidido por reglas, sin necesitar modelo.

## Qué hace

- `ResponseFormat` lleva un JSON Schema generado de la struct Go `WeatherReport` con `jsonschema.For[T]()`.
- Tras la llamada a tool, el agent devuelve JSON que se interpreta en `state["structured_response"]` y se imprime como valor tipado.

## Cómo aplicarlo

```go
agents.Create(agents.Options{
    Model: model, Tools: tools,
    ResponseFormat: &models.ResponseFormat{Name: "WeatherReport", Schema: jsonschema.For[WeatherReport]().Map()},
})
// out["structured_response"] holds the parsed object
```

Úsalo siempre que otro programa, no una persona, consuma la respuesta. Mantén correctas las tags de la struct (`json`, `description`): son las instrucciones que ve el modelo.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/structured_output
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un proveedor que soporte salida estructurada; comprueba que `structured_response` esté presente en todos los caminos, incluidos los errores de tool.
2. Valida la struct decodificada (rangos, enums) tras decodificar; cumplir el schema no es corrección de negocio. Ver `extraction_retries`.
3. Versiona el schema; cambiar un campo es un cambio de API para todos los consumidores.
4. Monitoriza los fallos de parsing como una tasa y alerta ante los aumentos.

## Ver también

- [Manual, capítulo 4: Prompts, chains y parsers](../../../docs/es-ES/manual/04-chains-prompts-parsers.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
