# 10. Deep Agents

`deepagents.Create` constrói um agent para tarefas longas e complexas. Sobre o `agents.Create`, ele soma:

- **Planejamento**: tool `write_todos`.
- **Filesystem virtual**: `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep` (e `execute` em backends com shell).
- **Subagents**: tool `task` delega trabalho a agents isolados.
- **Sumarização automática** do contexto.
- **Memória** (`AGENTS.md`) e **skills** (`SKILL.md`).
- **Human-in-the-loop** opcional e **permissões** de arquivo.

## 10.1 Criar

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "Você é um analista de pesquisa.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critico",
		Description:  "Revisa rascunhos e aponta falhas.",
		SystemPrompt: "Você é um revisor rigoroso.",
	}},
	Memory:       []string{"/AGENTS.md"}, // injetado no prompt, se existir no backend
	Skills:       []string{"/skills/"},   // cada pasta com SKILL.md (name/description no front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Escreva um relatório sobre baterias de sódio."})
for path, f := range backends.FilesFromState(out) { // arquivos criados pelo agent
	fmt.Println(path, len(f.Content))
}
```

### Opções

| Campo | Função |
|---|---|
| `Model` | **Obrigatório** |
| `Tools` | Tools extras do agent principal |
| `SystemPrompt` | Prompt base |
| `Backend` | Onde ficam os arquivos (padrão `backends.State{}`) |
| `SubAgents` | Subagents especializados (`SubAgentSpec`) |
| `Permissions` | Regras de acesso a arquivos |
| `Memory` | Caminhos de arquivos de memória carregados no prompt |
| `Skills` | Diretórios de skills |
| `InterruptOn` | Tools que exigem aprovação humana (`map[string]middleware.ToolPolicy`) |
| `SummarizeAt` | Tokens que disparam a sumarização (padrão 60000; `< 0` desativa) |
| `Middleware` | Middlewares extras (`agents.Middleware`) |
| `ResponseFormat` | Saída estruturada |
| `Checkpointer`, `Store` | Persistência e memória de longo prazo |

## 10.2 Subagents

Sempre existe um subagent `general-purpose`. Cada chamada à tool `task` roda um agent **isolado** e devolve só a resposta final ao principal, mantendo o contexto principal pequeno.

```go
type SubAgentSpec struct {
	Name         string
	Description  string               // o que o agent principal lê para decidir delegar
	SystemPrompt string
	Tools        []tools.Tool         // padrão: as tools do agent principal
	Model        models.ChatModel     // padrão: o modelo principal (permite modelo mais barato)
	Middleware   []agents.Middleware
}
```

Boas práticas: `Description` clara sobre *quando* usar; subagents de pesquisa em paralelo (várias chamadas `task` na mesma resposta); modelo menor para tarefas simples. Exemplo: `deepagents/research`.

## 10.3 Backends de arquivos (`deepagents/backends`)

Um `Backend` oferece `Ls`, `Read`, `Write`, `Edit`, `Glob` e `Grep`.

| Backend | Onde ficam os arquivos |
|---|---|
| `backends.State{}` (padrão) | No estado do grafo, por thread |
| `backends.Store{Store: s, Namespace: ...}` | Em um `store.Store`, compartilhado entre threads |
| `backends.Filesystem{Root: "./work"}` | Em disco, confinado à raiz (bloqueia `..` e symlinks que escapam) |
| `backends.LocalShell{Filesystem: ..., Env: ..., MaxOutput: ..., AllowedCommands: ..., Quiet: ...}` | Disco + tool `execute` (cwd fixo = `Root`) |
| `backends.Composite{Default, Routes}` | Roteia por prefixo |
| `backends.Remote` | Qualquer "executar comando em algum lugar" vira backend completo |

> **Atenção (segurança):** `LocalShell` **executa comandos no host, sem sandbox**. Na primeira execução o processo registra um aviso (`slog`); `Quiet: true` o silencia. `AllowedCommands` limita os programas iniciados (primeira palavra de cada segmento, após `VAR=valor`; `$(...)`, crases e substituições de processo são recusados), mas não filtra argumentos nem redirecionamentos: não inclua shells, interpretadores, `env`, `xargs` ou `find`. Para isolamento, use `kubernetes.Sandbox` ou outro runner: qualquer runner que execute comandos vira backend completo com `backends.Remote`.

### Composição por prefixo

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{
		"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}},
	},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

Tudo em `/memories/` persiste entre threads; o resto vive no estado da thread.

### Sandbox em pod do Kubernetes

```go
sb, _ := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
defer sb.Close(ctx)
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

Exemplo offline com um runner qualquer: `integrations/remote_sandbox`.

## 10.4 Permissões

Regras por operação e caminho. **A primeira regra que casa vale**; valem também para subagents.

```go
deepagents.Options{
	Model:   m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs somente leitura
	},
}
```

- `OpRead`: `ls`, `read_file`, `glob`, `grep`. `OpWrite`: `write_file`, `edit_file`.
- `Operations` vazio = ambas. `Paths` aceita globs (`**`).
- Sem regra que case, a operação é permitida. Para "negar tudo exceto...", coloque as exceções antes e termine com uma regra `Deny: true` para `/**`.
- **`execute` é bloqueado assim que existe qualquer regra `Deny`**, porque um shell contornaria as regras de caminho. Com permissões restritivas, o agent fica sem shell.

## 10.5 Memória e skills

- **Memória** (`Memory: []string{"/AGENTS.md"}`): o conteúdo do arquivo entra no prompt a cada chamada, se existir no backend. Use para convenções do projeto e preferências.
- **Skills** (`Skills: []string{"/skills/"}`): cada subpasta contém um `SKILL.md` com `name` e `description` no front matter. O agent vê a lista e lê a skill completa só quando precisa.

```
/skills/
  escrever-post/
    SKILL.md      ← front matter: name, description; corpo: instruções
```

Exemplo: `deepagents/content_builder` (com arquivos reais em disco).

## 10.6 Aprovação humana

```go
deepagents.Options{
	Model: m,
	InterruptOn: map[string]middleware.ToolPolicy{"execute": {}, "write_file": {}},
	Checkpointer: saver, // obrigatório
}
```

A retomada usa o mesmo protocolo do capítulo 9 (`HITLResponse`).

## 10.7 Middlewares dos Deep Agents (`deepagents/middleware`)

Passados em `Options.Middleware`:

### `Rubric`: auto-avaliação por critérios

```go
dmw.Rubric{
	Grader:        m,
	Criteria:      []string{"Cita uma fonte para cada número", "Tem resumo de 3 linhas"},
	MaxIterations: 3, // padrão 3
}
```

Quando o agent iria encerrar, um modelo "grader" avalia a transcrição contra os critérios. Se algum falha, o feedback volta ao agent para revisar. O resultado fica em `state["rubric_evaluations"]` (`satisfied`, `needs_revision`, `failed`, `max_iterations_reached` ou `grader_error`). Exemplo: `deepagents/rubric`.

### `AsyncSubagents`: trabalho em segundo plano

```go
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "pesquisador", Description: "Pesquisas longas.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// ou Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "pesquisador"},
}}
```

O agent ganha as tools `start`, `check`, `update`, `cancel` e `list` para disparar e acompanhar tarefas, sem bloquear. `LocalRunner` usa goroutines; `RemoteRunner` usa um Agent Server (capítulo 11). Estado das tarefas: `state["async_tasks"]`. Exemplos: `async_subagents`, `async_subagent_server`.

## 10.8 Padrões de uso

| Objetivo | Receita |
|---|---|
| Pesquisa com relatório | Subagents de pesquisa + `write_file` do relatório + `Rubric` (`research`) |
| Text-to-SQL | Plano + schema em arquivo + subagent validador (`text_to_sql`) |
| Loop "Ralph" | Contexto novo a cada iteração; estado só em arquivos (`ralph_mode`) |
| Documentação de MCP | Tools de um servidor MCP no agent (`mcp_docs_agent`) |

## 10.9 Cuidados

- Defina `Permissions` quando o backend é um disco real.
- Prompts longos + muitos arquivos aumentam custo: ajuste `SummarizeAt` e use subagents para isolar contexto.
- Limite recursão e custo com `ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: ...})` e middlewares `ModelCallLimit`.

Próximo: [11. MCP, servidor e graphs remotos](11-mcp-remote-servidor.md).
