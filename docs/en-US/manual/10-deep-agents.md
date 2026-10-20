# 10. Deep Agents

**English** | [Português](../../pt-BR/manual/10-deep-agents.md) | [Español](../../es-ES/manual/10-deep-agents.md)

`deepagents.Create` builds an agent for long, complex tasks. On top of `agents.Create`, it adds:

- **Planning**: the `write_todos` tool.
- **Virtual filesystem**: `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep` (and `execute` on backends with a shell).
- **Subagents**: the `task` tool delegates work to isolated agents.
- **Automatic summarization** of the context.
- **Memory** (`AGENTS.md`) and **skills** (`SKILL.md`).
- Optional **human-in-the-loop** and file **permissions**.

## 10.1 Creating

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "You are a research analyst.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critic",
		Description:  "Reviews drafts and points out flaws.",
		SystemPrompt: "You are a strict reviewer.",
	}},
	Memory:       []string{"/AGENTS.md"}, // injected into the prompt, if it exists in the backend
	Skills:       []string{"/skills/"},   // each folder with SKILL.md (name/description in the front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Write a report on sodium batteries."})
for path, f := range backends.FilesFromState(out) { // files created by the agent
	fmt.Println(path, len(f.Content))
}
```

### Options

| Field | Role |
|---|---|
| `Model` | **Required** |
| `Tools` | Extra tools for the main agent |
| `SystemPrompt` | Base prompt |
| `Backend` | Where the files live (default `backends.State{}`) |
| `SubAgents` | Specialized subagents (`SubAgentSpec`) |
| `Permissions` | File access rules |
| `Memory` | Memory file paths loaded into the prompt |
| `Skills` | Skill directories |
| `InterruptOn` | Tools that require human approval (`map[string]middleware.ToolPolicy`) |
| `SummarizeAt` | Tokens that trigger summarization (default 60000; `< 0` disables it) |
| `Middleware` | Extra middleware (`agents.Middleware`) |
| `ResponseFormat` | Structured output |
| `Checkpointer`, `Store` | Persistence and long-term memory |

## 10.2 Subagents

There is always a `general-purpose` subagent. Each call to the `task` tool runs an **isolated** agent and returns only the final answer to the main agent, keeping the main context small.

```go
type SubAgentSpec struct {
	Name         string
	Description  string               // what the main agent reads to decide to delegate
	SystemPrompt string
	Tools        []tools.Tool         // default: the main agent's tools
	Model        models.ChatModel     // default: the main model (allows a cheaper model)
	Middleware   []agents.Middleware
}
```

Best practices: a clear `Description` about *when* to use it; research subagents in parallel (several `task` calls in the same response); a smaller model for simple tasks. Example: `deepagents/research`.

## 10.3 File backends (`deepagents/backends`)

A `Backend` offers `Ls`, `Read`, `Write`, `Edit`, `Glob` and `Grep`.

| Backend | Where the files live |
|---|---|
| `backends.State{}` (default) | In the graph state, per thread |
| `backends.Store{Store: s, Namespace: ...}` | In a `store.Store`, shared across threads |
| `backends.Filesystem{Root: "./work"}` | On disk, confined to the root (blocks `..` and symlinks that escape) |
| `backends.LocalShell{Filesystem: ..., Env: ..., MaxOutput: ..., AllowedCommands: ..., Quiet: ...}` | Disk + `execute` tool (fixed cwd = `Root`) |
| `backends.Composite{Default, Routes}` | Routes by prefix |
| `backends.Remote` | Any "run a command somewhere" becomes a full backend |

> **Warning (security):** `LocalShell` **runs commands on the host, with no sandbox**. On the first execution the process logs a warning (`slog`); `Quiet: true` silences it. `AllowedCommands` limits the programs started (the first word of each segment, after `VAR=value`; `$(...)`, backticks and process substitutions are refused), but it does not filter arguments or redirections: do not include shells, interpreters, `env`, `xargs` or `find`. For isolation, use `kubernetes.Sandbox` or another runner: any runner that executes commands becomes a full backend with `backends.Remote`.

### Composition by prefix

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{
		"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}},
	},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

Everything under `/memories/` persists across threads; the rest lives in the thread's state.

### Sandbox in a Kubernetes pod

```go
sb, _ := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
defer sb.Close(ctx)
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

Offline example with any runner: `integrations/remote_sandbox`.

## 10.4 Permissions

Rules per operation and path. **The first matching rule wins**; they also apply to subagents.

```go
deepagents.Options{
	Model:   m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs are read-only
	},
}
```

- `OpRead`: `ls`, `read_file`, `glob`, `grep`. `OpWrite`: `write_file`, `edit_file`.
- An empty `Operations` means both. `Paths` accepts globs (`**`).
- With no matching rule, the operation is allowed. To "deny everything except...", put the exceptions first and end with a `Deny: true` rule for `/**`.
- **`execute` is blocked as soon as any `Deny` rule exists**, because a shell would bypass the path rules. With restrictive permissions, the agent has no shell.

## 10.5 Memory and skills

- **Memory** (`Memory: []string{"/AGENTS.md"}`): the file's content enters the prompt on every call, if it exists in the backend. Use it for project conventions and preferences.
- **Skills** (`Skills: []string{"/skills/"}`): each subfolder contains a `SKILL.md` with `name` and `description` in the front matter. The agent sees the list and reads the full skill only when it needs it.

```
/skills/
  write-post/
    SKILL.md      ← front matter: name, description; body: instructions
```

Example: `deepagents/content_builder` (with real files on disk).

## 10.6 Human approval

```go
deepagents.Options{
	Model: m,
	InterruptOn: map[string]middleware.ToolPolicy{"execute": {}, "write_file": {}},
	Checkpointer: saver, // required
}
```

Resuming uses the same protocol as chapter 9 (`HITLResponse`).

## 10.7 Deep Agents middleware (`deepagents/middleware`)

Passed in `Options.Middleware`:

### `Rubric`: self-evaluation by criteria

```go
dmw.Rubric{
	Grader:        m,
	Criteria:      []string{"Cites a source for every number", "Has a 3-line summary"},
	MaxIterations: 3, // default 3
}
```

When the agent is about to finish, a "grader" model evaluates the transcript against the criteria. If any fails, the feedback goes back to the agent to revise. The result is in `state["rubric_evaluations"]` (`satisfied`, `needs_revision`, `failed`, `max_iterations_reached` or `grader_error`). Example: `deepagents/rubric`.

### `AsyncSubagents`: background work

```go
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "researcher", Description: "Long research.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// or Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "researcher"},
}}
```

The agent gets the `start`, `check`, `update`, `cancel` and `list` tools to launch and follow tasks without blocking. `LocalRunner` uses goroutines; `RemoteRunner` uses an Agent Server (chapter 11). Task state: `state["async_tasks"]`. Examples: `async_subagents`, `async_subagent_server`.

## 10.8 Usage patterns

| Goal | Recipe |
|---|---|
| Research with a report | Research subagents + `write_file` for the report + `Rubric` (`research`) |
| Text-to-SQL | Plan + schema in a file + validator subagent (`text_to_sql`) |
| "Ralph" loop | Fresh context on each iteration; state only in files (`ralph_mode`) |
| MCP documentation | Tools from an MCP server in the agent (`mcp_docs_agent`) |

## 10.9 Cautions

- Set `Permissions` when the backend is a real disk.
- Long prompts + many files increase the cost: adjust `SummarizeAt` and use subagents to isolate context.
- Limit recursion and cost with `ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: ...})` and `ModelCallLimit` middleware.

---

Previous: [9. Agents and middleware](09-agents-e-middleware.md) · Next: [11. MCP, server and remote graphs](11-mcp-remote-servidor.md) · [Manual index](README.md)
