# text_to_sql

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un deep agent que planifica, valida una consulta con un subagent y la ejecuta con una tool SQL de solo lectura.

## Qué hace

- El líder escribe una lista de todos, anota el schema en `/schema.md`, pide a un subagent `query-validator` que revise el SQL y luego ejecuta `sql_query`.
- `StreamEvents` con `graph.ModeUpdates` imprime cada llamada a tool y la respuesta final a medida que ocurren.

## Cómo aplicarlo

Separa "escribir la consulta", "comprobar la consulta" y "ejecutar la consulta" para que una revisión de seguridad quede entre la generación y la ejecución.

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{sqlQuery},
    SubAgents: []deepagents.SubAgentSpec{{Name: "query-validator", Description: "Reviews SQL for safety.", Model: validator}},
})
```

Combínalo con la guarda de solo lectura que muestra `examples/langgraph/sql_agent`.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/text_to_sql
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Apunta `sql_query` a una réplica de solo lectura con un rol de base de datos que solo tenga `SELECT`; el prompt no es una frontera de seguridad, el rol sí.
2. Rechaza en código las sentencias que no sean `SELECT`, las sentencias múltiples y la falta de `LIMIT` antes de que la consulta llegue a la base de datos, y define un statement timeout.
3. Pon las notas del schema en una skill o archivo de memoria para que el agent no redescubra el schema en cada pregunta.
4. Arma un conjunto pequeño de preguntas con resultados conocidos y compara los números, no la redacción.
5. Registra cada consulta ejecutada con el usuario y el thread, para auditoría.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Manual, capítulo 5: Tools](../../../docs/es-ES/manual/05-tools.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
