# tool_calling

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Vincula tools a un modelo, inspecciona las llamadas a tools que hace y ejecútalas de forma concurrente con `ToolNode`.

## Qué hace

- Tres tools (`add`, `multiply`, `divide`) vinculadas con `models.BindTools`.
- El modelo guionizado pide cuatro llamadas a tools a la vez, incluidas `divide` por cero y una tool desconocida (`sqrt`).
- `prebuilt.NewToolNode` las ejecuta de forma concurrente; el ejemplo imprime cada resultado y estado, mostrando que los errores y las tools desconocidas vuelven como mensajes de error en lugar de tumbar la ejecución.

## Cómo aplicarlo

```go
model := models.BindTools(chatModel, tools...)
ai, _ := model.Generate(ctx, msgs)                // ai.ToolCalls
out, _ := prebuilt.NewToolNode(tools...).Node()(ctx, graph.State{"messages": []messages.Message{ai}})
```

Devuelve `tools.Errorf(...)` para fallos esperados, para que el modelo pueda recuperarse. Los ids de llamadas a tools deben ser únicos dentro de una conversación.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/tool_calling
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo real y comprueba que produce argumentos válidos para tus schemas; mejora las tags `description` donde no lo haga.
2. Define un timeout por tool y un límite de concurrencia si las tools llaman a APIs con rate limit.
3. Nunca pongas secretos en los resultados de las tools; vuelven al modelo y van a los traces.
4. Prueba los caminos de tool desconocida y de argumentos inválidos; ocurren en producción.

## Ver también

- [Manual, capítulo 5: Tools](../../../docs/es-ES/manual/05-tools.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
