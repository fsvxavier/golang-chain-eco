# reflection

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un generador y un crítico en bucle hasta que el crítico aprueba el borrador.

## Qué hace

- `generate` escribe una descripción de una frase; `reflect` la critica y devuelve la crítica como mensaje humano; el bucle termina en `APPROVED` o cuando la conversación supera un límite de tamaño.
- El primer borrador guionizado es demasiado corto; el segundo pasa.

## Cómo aplicarlo

```go
AddConditionalEdges("reflect", func(_ context.Context, s graph.State) (any, error) {
    if strings.Contains(demo.Last(s), "APPROVED") { return graph.End, nil }
    return "generate", nil
})
```

Úsalo cuando una segunda pasada mejora la salida de forma fiable (escritura, código, resúmenes). Da al crítico criterios concretos; un crítico vago aprueba todo o pone peros para siempre.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/reflection
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos reales; un modelo distinto (a menudo más fuerte) como crítico funciona mejor que la autocrítica.
2. Mantén el límite de tamaño/iteraciones y mide con qué frecuencia se alcanza.
3. Compara coste con calidad: muestrea 20 tareas con y sin reflexión y pide a una persona o a un eval que elija la mejor.
4. Haz que la señal de aprobación sea estructurada en lugar de comparar una cadena.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
