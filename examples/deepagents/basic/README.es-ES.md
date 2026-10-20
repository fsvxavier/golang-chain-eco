# basic

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El deep agent más pequeño: herramientas de planificación, filesystem virtual y delegación a subagents listos para usar.

## Qué hace

- Crea un deep agent con un system prompt y un modelo que escribe `/notes.md` mediante la tool integrada `write_file`.
- El archivo vive en el estado del agent (filesystem virtual), no en disco; el ejemplo lo imprime con `backends.FilesFromState`.

## Cómo aplicarlo

`deepagents.Create` da al agent `write_todos`, las tools de archivo (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`) y una tool `task` para subagents, sin ningún cableado extra.

```go
agent, _ := deepagents.Create(deepagents.Options{Model: model, SystemPrompt: "You are a research assistant."})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Save a note about deep agents."})
files := backends.FilesFromState(out)
```

Empieza por aquí en tareas largas que necesitan un borrador; añade `Backend`, `SubAgents`, `Memory` y `Skills` a medida que la tarea crece (los otros ejemplos de deepagents).

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/basic
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Ejecuta con una clave real; ahora el modelo decide si escribe el archivo, así que comprueba que `/notes.md` existe y tiene sentido.
2. Elige el backend con cuidado: el backend de estado por defecto desaparece con la ejecución. Usa `backends.Filesystem{Root: dir}` para archivos reales (confinados a `dir`) o `backends.Remote` para un runner aislado.
3. Nunca expongas `backends.LocalShell` a entrada no confiable; usa `kubernetes.Sandbox` u otro runner aislado.
4. Añade un checkpointer y un `thread_id` para que las ejecuciones largas sobrevivan a los reinicios, y define un límite de recursión explícito.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Manual, capítulo 14: Producción, seguridad y troubleshooting](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
