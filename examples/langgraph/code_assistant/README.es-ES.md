# code_assistant

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Genera código, lo comprueba, devuelve el error y reintenta hasta que la comprobación pase.

## Qué hace

- `generate` pide al modelo codificador; `check` valida el resultado (aquí un sustituto en Go que exige `func Add(` y el comportamiento correcto).
- Si falla, el error entra en la conversación y el grafo vuelve, hasta 4 iteraciones (`graph.Sum()` las cuenta).
- El codificador guionizado falla dos veces (nombre equivocado, operador equivocado) y acierta al tercer intento.

## Cómo aplicarlo

Cambia el `check` sustituto por uno real: escribe el código en un módulo temporal, ejecuta `go build ./... && go test ./...` con timeout y devuelve la salida del compilador o de las pruebas como error.

```go
AddConditionalEdges("check", func(_ context.Context, s graph.State) (any, error) {
    if s["error"] != "" && s["iterations"].(int) < 4 { return "generate", nil }
    return graph.End, nil
})
```

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/code_assistant
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un codificador real y un verificador real. Ejecuta el verificador en un sandbox aislado (contenedor o `kubernetes.Sandbox`) sin red, con límite de CPU/memoria y timeout de reloj: estás ejecutando código escrito por un modelo.
2. Mantén el límite de iteraciones; registra cada intento y el error que lo provocó.
3. Trunca las salidas largas del compilador antes de devolverlas para que el contexto no se llene de ruido.
4. Nunca hagas merge de código generado sin revisión; usa esto como generador de borradores.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
