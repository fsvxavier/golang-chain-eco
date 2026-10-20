# research

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent líder planifica, delega dos preguntas de investigación a subagents en paralelo y escribe un informe en un archivo.

## Qué hace

- El líder llama a `write_todos`, luego a dos `task` al subagent `researcher` a la vez y, por último, a `write_file` para `/report.md`.
- El subagent tiene su propio modelo y la tool `internet_search` (un stub aquí).
- Imprime todos los archivos del filesystem virtual, la lista de todos y el mensaje final.

## Cómo aplicarlo

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{search},
    SubAgents: []deepagents.SubAgentSpec{{Name: "researcher", Description: "Focused researcher.", Model: sub}},
})
```

Da a cada subagent una descripción estrecha (el líder la usa para decidir quién recibe el trabajo), un modelo propio si es más barato y solo las tools que necesita. Las llamadas `task` paralelas en un mismo turno se ejecutan de forma concurrente.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/research
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Cambia el stub `internet_search` por una tool de búsqueda real (cliente HTTP con timeout, tope de resultados y lista de URLs permitidas) y usa modelos reales para el líder y el subagent.
2. Usa un modelo más fuerte para el líder y uno más barato para los investigadores; compara el coste por informe.
3. Acota la ejecución: `ModelCallLimit`, `ToolCallLimit` y un `RecursionLimit`, porque un agent de investigación puede entrar en bucle de búsquedas.
4. Trata el texto obtenido de la web como entrada no confiable (prompt injection): mantén el paso de redacción del informe sin tools que puedan actuar sobre el mundo exterior.
5. Persiste los informes con `backends.Filesystem` o un object store en lugar de leerlos del estado.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
