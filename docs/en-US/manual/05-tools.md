# 5. Tools

**English** | [Português](../../pt-BR/manual/05-tools.md) | [Español](../../es-ES/manual/05-tools.md)

Tools are Go functions that the model can ask to run. The `core/tools` package takes care of the schema, validation and conversion of results.

## 5.1 Creating a typed tool

```go
type SearchArgs struct {
	Query string `json:"query" description:"Search term"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Searches the knowledge base.",
	func(ctx context.Context, a SearchArgs) (any, error) {
		return doSearch(a.Query, a.Limit)
	})
```

- The **name** and the **description** are what the model reads to decide when to use the tool. Be specific.
- The arguments struct is converted into a JSON Schema by reflection.
- The argument arrives already validated and decoded; the `any` result is converted into text (`tools.Stringify`) for the model.

## 5.2 Argument schema

| Tag | Effect |
|---|---|
| `json:"name"` | Field name; `omitempty` makes it optional |
| `description:"..."` | Field description (read by the model) |
| `jsonschema:"enum=a\|b\|c"` | Allowed values |
| `jsonschema:"minimum=1,maximum=20"` | Numeric range |
| `jsonschema:"required"` | Marks as required |
| `jsonschema:"default=x"` | Default value |
| `jsonschema:"description=..."` | Alternative description |

Fields without `omitempty` are treated as required. The runtime validates the received arguments against the schema before calling the function.

### Manual schema

When the struct is not enough, use `NewRaw` and supply the ready schema:

```go
t := tools.NewRaw("run_query", "Runs read-only SQL.",
	&jsonschema.Schema{ /* ... */ },
	func(ctx context.Context, args map[string]any) (any, error) {
		return runQuery(args["sql"].(string))
	})
```

To decode a `map[string]any` into a struct: `tools.Decode(args, &myStruct)`.

## 5.3 Errors

There are two kinds of error, with different effects:

```go
// "Model" error: comes back as a tool message and the loop continues.
// The model can fix the arguments and try again.
return nil, tools.Errorf("city %q not found", a.City)

// Common error: treated by the agent as an execution failure.
return nil, fmt.Errorf("database unavailable: %w", err)
```

Use `tools.Errorf` for invalid inputs or expected cases. Use a common error for infrastructure failures. The handling is configurable in `prebuilt.ToolNode` through `ErrorHandler`; the default is `prebuilt.DefaultErrorHandler`.

## 5.4 Terminal tool

A terminal tool ends the agent loop, using its result as the final answer:

```go
finish := tools.New("finish", "Ends and returns the final answer.", fn, tools.WithReturnDirect())
```

## 5.5 Result with an artifact

When the result has one part for the model and another only for your code (a file, a large object):

```go
return tools.ContentAndArtifact{
	Content:  "Chart generated with 120 points.", // goes to the model
	Artifact: chartBytes,                          // stays in Message.Artifact
}, nil
```

## 5.6 Changing the state from a tool

A tool can return `graph.Command` to update the graph state:

```go
return graph.Command{Update: graph.State{"user_name": "Ana"}}, nil
```

Inside the tool:

- `prebuilt.StateOf(ctx)` returns the current graph state.
- `tools.CallID(ctx)` returns the call ID (to build a tool message of your own).

## 5.7 Using tools with a model directly

```go
bound := models.BindTools(m, search, weather)
resp, _ := bound.Generate(ctx, msgs)
for _, call := range resp.ToolCalls {
	t, _ := tools.Find(allTools, call.Name)
	out, err := t.Call(ctx, call.Args) // manual execution
	...
}
```

In practice, let `agents.Create` or `prebuilt.ToolNode` run this loop (chapter 9). To choose the model's behavior: `models.WithToolChoice("any")` forces a call; `"none"` forbids it.

## 5.8 Ready-made tools in the project

| Source | Tool |
|---|---|
| `retrievers.AsTool(retriever, name, description)` | Search in a retriever (agentic RAG) |
| `search.Tool(name, description, searcher, n)` | Web search (Tavily, Exa, Brave...) |
| `mcp.LoadTools(...)` | Tools from MCP servers |
| `jenkins`, `kubernetes`, `salesforce`, `messaging`, `aws` (Lambda) | Actions in external systems |
| `todo` and the filesystem of the Deep Agents | `write_todos`, `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `task` |

> **Warning:** tools that send SMS, run shell commands, read CRM data or trigger builds act on real systems. Restrict scope and require human approval (chapter 9, `HumanInTheLoop`).

## 5.9 Best practices

1. One tool, one responsibility. Names in `snake_case`, clear verbs (`get_weather`, `create_ticket`).
2. Describe each field with `description`; the model uses it to fill in the arguments.
3. Return short, useful results. Huge results consume context; use `ContentAndArtifact` or summarize.
4. Make tools with side effects idempotent when possible, since a retry may repeat the call.
5. Validate business rules inside the tool and answer with `tools.Errorf` so the model can correct itself.

---

Previous: [4. Prompts, chains and parsers](04-chains-prompts-parsers.md) · Next: [6. RAG](06-rag.md) · [Manual index](README.md)
