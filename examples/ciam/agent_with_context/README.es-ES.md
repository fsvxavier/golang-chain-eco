# agent_with_context

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent que consulta el gestor de contexto del proyecto antes de cambiar código y, así, respeta una decisión que nadie mencionó.

El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, y todo se decide por reglas, sin necesidad de modelo.

## Qué hace

- Crea un proyecto con un tipo de factura y un ADR que dice "nunca uses float para dinero".
- El agent tiene las tools de `ciam` (`kit.All()`) y las instrucciones de ciam como system prompt. Antes de implementar, llama a `ciam_decision_context` y recibe el código **y** el ADR, así que propone `DiscountCents int64` en lugar de un porcentaje float.

## Cómo aplicarlo

```go
kit := ciam.Toolkit{M: mgr, Project: root}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

El system prompt (`ciam.Instructions`) dice al agent cuándo consultar el contexto. Añade las tools a cualquier agent, incluidos los deep agents y el ingeniero de Open SWE.

## Cómo ejecutarlo

```bash
go run ./examples/ciam/agent_with_context
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa un modelo real y un proyecto real indexado con un embedder real; comprueba que el modelo realmente llama primero a `ciam_decision_context` (mira el trace de llamadas a tools).
2. Siembra los ADR que importan (dinero, autenticación, retención de datos) y prueba prompts que los violarían.
3. Mide los tokens por tarea con y sin las tools.
4. Mantén el índice al día (vuelve a ejecutar `Index` tras los merges).

## Ver también

- [Manual, capítulo 18: ciam: contexto de proyecto barato y preciso para asistentes](../../../docs/es-ES/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/es-ES/CIAM.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
