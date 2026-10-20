# 9. Agents e middleware

[English](../../en-US/manual/09-agents-e-middleware.md) | **Português** | [Español](../../es-ES/manual/09-agents-e-middleware.md)

`agents.Create` monta o loop clássico de agent: o modelo decide, as tools executam, o resultado volta ao modelo, até haver uma resposta final. É o equivalente de `create_agent` do LangChain.

## 9.1 Criar um agent

```go
agent, err := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{search, sendEmail},
	SystemPrompt: "Você é um assistente de atendimento.",
	Middleware:   []agents.Middleware{ /* ver 9.4 */ },
	Compile:      []graph.CompileOption{graph.WithCheckpointer(saver)},
})
```

| Campo | Função |
|---|---|
| `Model` | **Obrigatório.** Qualquer `models.ChatModel` |
| `Tools` | Tools disponíveis (as dos middlewares são somadas) |
| `SystemPrompt` | Prompt de sistema |
| `Middleware` | Lista ordenada de middlewares |
| `ResponseFormat` | Pede saída JSON; resultado em `state["structured_response"]` |
| `StateSchema` | Canais extras de estado (`graph.Schema`) |
| `Compile` | Opções de compilação do grafo (checkpointer, store, interrupts, cache) |

O retorno é um `*graph.Compiled`: tudo do capítulo 7 e 8 vale (`Invoke`, `Stream`, `StreamEvents`, `GetState`, threads...).

## 9.2 Executar

```go
out, err := agent.Invoke(ctx, graph.State{"messages": "Como está o tempo em Lisboa?"})
msgs := out["messages"].([]messages.Message)
fmt.Println(msgs[len(msgs)-1].Text())
```

A entrada `messages` aceita `string`, `messages.Message` ou `[]messages.Message`. Com checkpointer e `graph.Thread(id)` no contexto, as chamadas seguintes continuam a conversa.

### Streaming

```go
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": "oi"}, graph.ModeMessages) {
	if err != nil {
		log.Fatal(err)
	}
	if msg, ok := ev.Data.(messages.Message); ok {
		fmt.Print(msg.Text())
	}
}
```

## 9.3 Fluxo interno e pontos de extensão

```
before_agent → [ before_model → model → after_model ] ⇄ tools → after_agent
```

Um middleware implementa `Name() string` e qualquer combinação destes hooks:

| Hook | Quando roda |
|---|---|
| `BeforeAgent(ctx, st)` | Uma vez, antes de tudo |
| `BeforeModel(ctx, st)` | Antes de cada chamada ao modelo |
| `WrapModelCall(ctx, req, next)` | Em volta da chamada ao modelo (pode alterar `req` ou trocar o resultado) |
| `AfterModel(ctx, st)` | Depois de cada resposta do modelo |
| `WrapToolCall(ctx, call, next)` | Em volta de cada execução de tool |
| `AfterAgent(ctx, st)` | Uma vez, no final |
| `Tools() []tools.Tool` | (opcional) adiciona tools |
| `StateSchema() graph.Schema` | (opcional) adiciona canais de estado |

Hooks de estado devolvem `nil`, uma atualização `graph.State` ou um `graph.Command`. Para desviar o fluxo, inclua `jump_to`:

```go
return graph.State{"jump_to": agents.JumpEnd}, nil // ou agents.JumpModel, agents.JumpTools
```

## 9.4 Middlewares incluídos (`agents/middleware`)

```go
Middleware: []agents.Middleware{
	middleware.ModelCallLimit{RunLimit: 10},
	middleware.ToolRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
	middleware.Summarization{Model: m, MaxTokens: 8000, KeepMessages: 12},
	middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail, middleware.PIICreditCard}, Strategy: middleware.PIIRedact},
	middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
},
```

| Middleware | Para quê | Campos principais |
|---|---|---|
| `ModelCallLimit` | Limita chamadas ao modelo (custo, loops) | `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"end"` padrão ou `"error"`) |
| `ToolCallLimit` | Limita chamadas de tool (de uma ou de todas) | `ToolName`, `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"continue"`, `"error"`, `"end"`) |
| `ModelFallback` | Tenta outros modelos se o principal falhar | `Fallbacks []models.ChatModel` |
| `ModelRetry` | Repete a chamada ao modelo com backoff | `Options runnables.RetryOptions` |
| `ToolRetry` | Repete a execução de tools | `Options`, `Tools` (restringe a alguns nomes) |
| `Summarization` | Resume o histórico quando passa de um limite | `Model`, `MaxTokens` (4000), `KeepMessages` (20), `Counter`, `Prompt` |
| `ContextEditing` | Limpa resultados antigos de tools | `TriggerTokens` (100000), `Keep` (3), `Placeholder` |
| `TodoList` | Dá ao agent a tool `write_todos` e a chave `todos` | `SystemPrompt` |
| `PII` | Remove dados pessoais da entrada | `Kinds`, `Strategy` |
| `HumanInTheLoop` | Pausa para aprovar/editar/rejeitar tools | `Tools map[string]ToolPolicy` |

### `PII`

Detectores prontos: `PIIEmail`, `PIICreditCard` (e outros em `context.go`). Estratégias:

| Constante | Resultado |
|---|---|
| `middleware.PIIRedact` (padrão) | `[REDACTED_EMAIL]` |
| `middleware.PIIMask` | `****1234` |
| `middleware.PIIBlock` | Erro `*middleware.PIIError` |

Detectores próprios: `middleware.PIIKind{Name: "cpf", Pattern: regexp.MustCompile(...)}`. O histórico guardado já contém o texto sanitizado.

> A detecção é por expressão regular: ajuda, mas não substitui revisão de privacidade. Valide com seus dados reais.

### `Summarization` e `ContextEditing`

Controlam o tamanho do contexto em conversas longas. `Summarization` substitui mensagens antigas por um resumo gerado pelo modelo; `ContextEditing` apenas limpa resultados antigos de tools (mais barato, sem chamada ao modelo). Podem ser usados juntos.

## 9.5 Aprovação humana de tools (`HumanInTheLoop`)

Requer checkpointer e `thread_id`.

```go
policy := map[string]middleware.ToolPolicy{
	"send_email":   {},                                                   // aprovar, editar ou rejeitar
	"delete_file":  {Allowed: []string{middleware.Approve, middleware.Reject}}, // sem edição
}

out, _ := agent.Invoke(ctx, graph.State{"messages": "Envie o relatório para o Bob"})
// pausa antes de send_email; o pedido vem em graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// ou {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: map[string]any{...}}}
	// ou {Type: middleware.Reject, Message: "não envie"}
}})
```

- `Interrupt.Value` é um `middleware.HITLRequest` com as `ActionRequests` (nome, argumentos, descrição) e, por tool, as decisões permitidas.
- Responda com **uma decisão por ação**, na mesma ordem.
- `Reject` devolve ao modelo a mensagem informada; ele pode propor outra abordagem.
- Tools não listadas em `Tools` executam sem revisão.

Exemplos: `hitl_agent`, `customer_support`.

## 9.6 Middleware próprio

Implemente `Name()` e os hooks que quiser:

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }

func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools e req.Model podem ser alterados antes
	log.Println("modelo levou", time.Since(t))
	return out, err
}
```

Middleware que adiciona uma tool e estado:

```go
type Audit struct{ Log *slog.Logger }

func (Audit) Name() string { return "audit" }
func (Audit) StateSchema() graph.Schema { return graph.Schema{"audit": graph.Append()} }
func (a Audit) WrapToolCall(ctx context.Context, c messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	a.Log.Info("tool", "name", c.Name)
	return next(ctx, c)
}
```

`ModelRequest` expõe `Model`, `System`, `Messages`, `Tools`, `Options` e `State`: dá para trocar o modelo por consulta (roteamento por custo), filtrar tools por contexto ou injetar instruções.

### Ordem

Nos hooks `Wrap*`, o **primeiro** middleware da lista é o mais externo (chama os seguintes via `next`). Hooks `Before*`/`After*` rodam em sequência; não dependa da ordem entre eles. Combine conforme o efeito desejado (por exemplo, `ModelRetry` por fora de `ModelFallback` repete a cadeia inteira) e teste a combinação (veja `examples/langchain/middleware_stack`).

## 9.7 Saída estruturada em agents

```go
type Report struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

agent, _ := agents.Create(agents.Options{
	Model: m,
	ResponseFormat: &models.ResponseFormat{
		Name: "report",
		Schema: map[string]any{ // JSON Schema do tipo Report
			"type": "object",
			"properties": map[string]any{
				"title":   map[string]any{"type": "string"},
				"bullets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"title", "bullets"},
		},
	},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Resuma o artigo"})
raw := out["structured_response"] // parseado do JSON da resposta final
```

Exemplo completo: `examples/langgraph/structured_output`.

## 9.8 Testar agents sem rede

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisboa"}}),
	messages.AI("Ensolarado em Lisboa."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
out, _ := agent.Invoke(ctx, graph.State{"messages": "tempo em Lisboa?"})
// verifique out["messages"] e fake.Calls
```

## 9.9 Quando usar o quê

| Situação | Use |
|---|---|
| Loop mínimo modelo/tools | `prebuilt.CreateReactAgent` |
| Agent de produção com limites, retry, PII, aprovação | `agents.Create` + middleware |
| Fluxo com etapas fixas, ramificações e paralelismo | `graph.StateGraph` |
| Tarefas longas com planejamento, arquivos e subagents | `deepagents.Create` |

---

Anterior: [8. Persistência, HITL e streaming](08-persistencia-hitl-streaming.md) · Próximo: [10. Deep Agents](10-deep-agents.md) · [Índice do manual](README.md)
