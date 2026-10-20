# chatbot_simulation

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un usuario simulado conversa con tu asistente y un evaluador puntúa la conversación.

## Qué hace

- Tres roles en un grafo: `assistant`, `simulated_user` (un modelo que hace de cliente, con los roles invertidos) y `evaluate` (un evaluador que devuelve JSON con una nota).
- El bucle termina cuando el usuario simulado dice `FINISHED` o la conversación se hace larga.
- Imprime cada turno y el veredicto final.

## Cómo aplicarlo

Úsalo como harness de regresión de un chatbot: escribe personas y objetivos para el usuario simulado, ejecuta muchas conversaciones y compara las notas antes y después de un cambio de prompt o modelo.

```go
out, _ := grader.Generate(ctx, []messages.Message{messages.Human("Score this support chat 1-5 as JSON:\n" + messages.Buffer(ms))})
```

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/chatbot_simulation
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Cambia el asistente por tu agent real (el mismo que despliegas) y usa modelos reales para el usuario y el evaluador.
2. Arma 20+ escenarios (camino feliz, cliente enfadado, fuera de tema, adversarial) y ejecuta cada uno varias veces: las simulaciones son estocásticas.
3. Haz que el evaluador devuelva criterios estructurados, no una sola nota, y contrasta sus notas con valoraciones humanas por muestreo.
4. Ejecútalo en CI con cada cambio de prompt o modelo y rompe el build si la nota cae; ver también `examples/evals/basic`.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 12: Observabilidad](../../../docs/es-ES/manual/12-observabilidade.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
