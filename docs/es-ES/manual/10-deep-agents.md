# 10. Deep Agents

[English](../../en-US/manual/10-deep-agents.md) | [Português](../../pt-BR/manual/10-deep-agents.md) | **Español**

`deepagents.Create` construye un agent para tareas largas y complejas. Sobre `agents.Create`, añade:

- **Planificación**: tool `write_todos`.
- **Filesystem virtual**: `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep` (y `execute` en backends con shell).
- **Subagents**: la tool `task` delega trabajo a agents aislados.
- **Resumen automático** del contexto.
- **Memoria** (`AGENTS.md`) y **skills** (`SKILL.md`).
- **Human-in-the-loop** opcional y **permisos** de archivos.

## 10.1 Crear

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "Eres un analista de investigación.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critico",
		Description:  "Revisa borradores y señala fallas.",
		SystemPrompt: "Eres un revisor riguroso.",
	}},
	Memory:       []string{"/AGENTS.md"}, // se inyecta en el prompt, si existe en el backend
	Skills:       []string{"/skills/"},   // cada carpeta con SKILL.md (name/description en el front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Escribe un informe sobre baterías de sodio."})
for path, f := range backends.FilesFromState(out) { // archivos creados por el agent
	fmt.Println(path, len(f.Content))
}
```

### Opciones

| Campo | Función |
|---|---|
| `Model` | **Obligatorio** |
| `Tools` | Tools extra del agent principal |
| `SystemPrompt` | Prompt base |
| `Backend` | Dónde viven los archivos (predeterminado `backends.State{}`) |
| `SubAgents` | Subagents especializados (`SubAgentSpec`) |
| `Permissions` | Reglas de acceso a archivos |
| `Memory` | Rutas de archivos de memoria cargados en el prompt |
| `Skills` | Directorios de skills |
| `InterruptOn` | Tools que requieren aprobación humana (`map[string]middleware.ToolPolicy`) |
| `SummarizeAt` | Tokens que disparan el resumen (predeterminado 60000; `< 0` lo desactiva) |
| `Middleware` | Middlewares extra (`agents.Middleware`) |
| `ResponseFormat` | Salida estructurada |
| `Checkpointer`, `Store` | Persistencia y memoria de largo plazo |

## 10.2 Subagents

Siempre existe un subagent `general-purpose`. Cada llamada a la tool `task` ejecuta un agent **aislado** y devuelve solo la respuesta final al principal, manteniendo pequeño el contexto principal.

```go
type SubAgentSpec struct {
	Name         string
	Description  string               // lo que lee el agent principal para decidir delegar
	SystemPrompt string
	Tools        []tools.Tool         // predeterminado: las tools del agent principal
	Model        models.ChatModel     // predeterminado: el modelo principal (permite un modelo más barato)
	Middleware   []agents.Middleware
}
```

Buenas prácticas: una `Description` clara sobre *cuándo* usarlo; subagents de investigación en paralelo (varias llamadas `task` en la misma respuesta); un modelo más pequeño para tareas simples. Ejemplo: `deepagents/research`.

## 10.3 Backends de archivos (`deepagents/backends`)

Un `Backend` ofrece `Ls`, `Read`, `Write`, `Edit`, `Glob` y `Grep`.

| Backend | Dónde viven los archivos |
|---|---|
| `backends.State{}` (predeterminado) | En el estado del grafo, por thread |
| `backends.Store{Store: s, Namespace: ...}` | En un `store.Store`, compartido entre threads |
| `backends.Filesystem{Root: "./work"}` | En disco, confinado a la raíz (bloquea `..` y symlinks que escapan) |
| `backends.LocalShell{Filesystem: ..., Env: ..., MaxOutput: ..., AllowedCommands: ..., Quiet: ...}` | Disco + tool `execute` (cwd fijo = `Root`) |
| `backends.Composite{Default, Routes}` | Enruta por prefijo |
| `backends.Remote` | Cualquier "ejecutar un comando en algún lugar" se vuelve un backend completo |

> **Atención (seguridad):** `LocalShell` **ejecuta comandos en el host, sin sandbox**. En la primera ejecución el proceso registra un aviso (`slog`); `Quiet: true` lo silencia. `AllowedCommands` limita los programas iniciados (la primera palabra de cada segmento, tras `VAR=valor`; `$(...)`, comillas invertidas y sustituciones de proceso se rechazan), pero no filtra argumentos ni redirecciones: no incluyas shells, intérpretes, `env`, `xargs` ni `find`. Para aislamiento, usa `kubernetes.Sandbox` u otro runner: cualquier runner que ejecute comandos se convierte en un backend completo con `backends.Remote`.

### Composición por prefijo

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{
		"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}},
	},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

Todo lo que está en `/memories/` persiste entre threads; el resto vive en el estado del thread.

### Sandbox en un pod de Kubernetes

```go
sb, _ := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
defer sb.Close(ctx)
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

Ejemplo offline con un runner cualquiera: `integrations/remote_sandbox`.

## 10.4 Permisos

Reglas por operación y ruta. **Vale la primera regla que coincida**; valen también para los subagents.

```go
deepagents.Options{
	Model:   m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs de solo lectura
	},
}
```

- `OpRead`: `ls`, `read_file`, `glob`, `grep`. `OpWrite`: `write_file`, `edit_file`.
- `Operations` vacío = ambas. `Paths` acepta globs (`**`).
- Sin regla que coincida, la operación se permite. Para "negar todo excepto...", pon las excepciones primero y termina con una regla `Deny: true` para `/**`.
- **`execute` se bloquea en cuanto existe cualquier regla `Deny`**, porque un shell eludiría las reglas de ruta. Con permisos restrictivos, el agent se queda sin shell.

## 10.5 Memoria y skills

- **Memoria** (`Memory: []string{"/AGENTS.md"}`): el contenido del archivo entra en el prompt en cada llamada, si existe en el backend. Úsalo para convenciones del proyecto y preferencias.
- **Skills** (`Skills: []string{"/skills/"}`): cada subcarpeta contiene un `SKILL.md` con `name` y `description` en el front matter. El agent ve la lista y lee la skill completa solo cuando la necesita.

```
/skills/
  escribir-post/
    SKILL.md      ← front matter: name, description; cuerpo: instrucciones
```

Ejemplo: `deepagents/content_builder` (con archivos reales en disco).

## 10.6 Aprobación humana

```go
deepagents.Options{
	Model: m,
	InterruptOn: map[string]middleware.ToolPolicy{"execute": {}, "write_file": {}},
	Checkpointer: saver, // obligatorio
}
```

La reanudación usa el mismo protocolo del capítulo 9 (`HITLResponse`).

## 10.7 Middlewares de los Deep Agents (`deepagents/middleware`)

Se pasan en `Options.Middleware`:

### `Rubric`: autoevaluación por criterios

```go
dmw.Rubric{
	Grader:        m,
	Criteria:      []string{"Cita una fuente para cada número", "Tiene un resumen de 3 líneas"},
	MaxIterations: 3, // predeterminado 3
}
```

Cuando el agent iba a terminar, un modelo "evaluador" revisa la transcripción contra los criterios. Si alguno falla, el feedback vuelve al agent para revisar. El resultado queda en `state["rubric_evaluations"]` (`satisfied`, `needs_revision`, `failed`, `max_iterations_reached` o `grader_error`). Ejemplo: `deepagents/rubric`.

### `AsyncSubagents`: trabajo en segundo plano

```go
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "investigador", Description: "Investigaciones largas.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// o Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "investigador"},
}}
```

El agent gana las tools `start`, `check`, `update`, `cancel` y `list` para disparar y seguir tareas sin bloquearse. `LocalRunner` usa goroutines; `RemoteRunner` usa un Agent Server (capítulo 11). Estado de las tareas: `state["async_tasks"]`. Ejemplos: `async_subagents`, `async_subagent_server`.

## 10.8 Patrones de uso

| Objetivo | Receta |
|---|---|
| Investigación con informe | Subagents de investigación + `write_file` del informe + `Rubric` (`research`) |
| Text-to-SQL | Plan + schema en archivo + subagent validador (`text_to_sql`) |
| Loop "Ralph" | Contexto nuevo en cada iteración; estado solo en archivos (`ralph_mode`) |
| Documentación de MCP | Tools de un servidor MCP en el agent (`mcp_docs_agent`) |

## 10.9 Cuidados

- Define `Permissions` cuando el backend es un disco real.
- Prompts largos + muchos archivos aumentan el costo: ajusta `SummarizeAt` y usa subagents para aislar contexto.
- Limita recursión y costo con `ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: ...})` y middlewares `ModelCallLimit`.

---

Anterior: [9. Agents y middleware](09-agents-e-middleware.md) · Siguiente: [11. MCP, servidor y grafos remotos](11-mcp-remote-servidor.md) · [Índice del manual](README.md)
