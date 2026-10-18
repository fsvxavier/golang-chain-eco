# 5. Tools

Tools são funções Go que o modelo pode pedir para executar. O pacote `core/tools` cuida do schema, da validação e da conversão de resultados.

## 5.1 Criar uma tool tipada

```go
type SearchArgs struct {
	Query string `json:"query" description:"Termo de busca"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Busca na base de conhecimento.",
	func(ctx context.Context, a SearchArgs) (any, error) {
		return doSearch(a.Query, a.Limit)
	})
```

- O **nome** e a **descrição** são o que o modelo lê para decidir quando usar a tool. Seja específico.
- A struct de argumentos é convertida em JSON Schema por reflexão.
- O argumento chega já validado e decodificado; o resultado `any` é convertido em texto (`tools.Stringify`) para o modelo.

## 5.2 Schema dos argumentos

| Tag | Efeito |
|---|---|
| `json:"nome"` | Nome do campo; `omitempty` torna opcional |
| `description:"..."` | Descrição do campo (lida pelo modelo) |
| `jsonschema:"enum=a\|b\|c"` | Valores permitidos |
| `jsonschema:"minimum=1,maximum=20"` | Faixa numérica |
| `jsonschema:"required"` | Marca como obrigatório |
| `jsonschema:"default=x"` | Valor padrão |
| `jsonschema:"description=..."` | Descrição alternativa |

Campos sem `omitempty` são tratados como obrigatórios. O runtime valida os argumentos recebidos contra o schema antes de chamar a função.

### Schema manual

Quando a struct não basta, use `NewRaw` e entregue o schema pronto:

```go
t := tools.NewRaw("run_query", "Executa SQL somente leitura.",
	&jsonschema.Schema{ /* ... */ },
	func(ctx context.Context, args map[string]any) (any, error) {
		return runQuery(args["sql"].(string))
	})
```

Para decodificar `map[string]any` numa struct: `tools.Decode(args, &minhaStruct)`.

## 5.3 Erros

Existem dois tipos de erro, com efeitos diferentes:

```go
// Erro "do modelo": volta como mensagem de tool e o loop continua.
// O modelo pode corrigir os argumentos e tentar de novo.
return nil, tools.Errorf("cidade %q não encontrada", a.City)

// Erro comum: tratado pelo agent como falha de execução.
return nil, fmt.Errorf("banco indisponível: %w", err)
```

Use `tools.Errorf` para entradas inválidas ou casos esperados. Use erro comum para falhas de infraestrutura. O tratamento é configurável em `prebuilt.ToolNode` via `ErrorHandler`; o padrão é `prebuilt.DefaultErrorHandler`.

## 5.4 Tool terminal

Uma tool terminal encerra o loop do agent usando o resultado dela como resposta final:

```go
finish := tools.New("finish", "Encerra e devolve a resposta final.", fn, tools.WithReturnDirect())
```

## 5.5 Resultado com artefato

Quando o resultado tem uma parte para o modelo e outra só para o seu código (um arquivo, um objeto grande):

```go
return tools.ContentAndArtifact{
	Content:  "Gráfico gerado com 120 pontos.", // vai ao modelo
	Artifact: chartBytes,                         // fica em Message.Artifact
}, nil
```

## 5.6 Alterar o estado a partir de uma tool

Uma tool pode devolver `graph.Command` para atualizar o estado do grafo:

```go
return graph.Command{Update: graph.State{"user_name": "Ana"}}, nil
```

Dentro da tool:

- `prebuilt.StateOf(ctx)` devolve o estado atual do grafo.
- `tools.CallID(ctx)` devolve o ID da chamada (para montar uma mensagem de tool própria).

## 5.7 Usar tools com um modelo diretamente

```go
bound := models.BindTools(m, search, weather)
resp, _ := bound.Generate(ctx, msgs)
for _, call := range resp.ToolCalls {
	t, _ := tools.Find(allTools, call.Name)
	out, err := t.Call(ctx, call.Args) // execução manual
	...
}
```

Na prática você deixa `agents.Create` ou `prebuilt.ToolNode` fazer esse loop (capítulo 9). Para escolher o comportamento do modelo: `models.WithToolChoice("any")` obriga uma chamada; `"none"` proíbe.

## 5.8 Tools prontas do projeto

| Origem | Tool |
|---|---|
| `retrievers.AsTool(retriever, nome, descrição)` | Busca em um retriever (RAG agêntico) |
| `search.Tool(nome, descrição, searcher, n)` | Busca na web (Tavily, Exa, Brave...) |
| `mcp.LoadTools(...)` | Tools de servidores MCP |
| `jenkins`, `kubernetes`, `salesforce`, `messaging`, `aws` (Lambda) | Ações em sistemas externos |
| `todo` e filesystem dos Deep Agents | `write_todos`, `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `task` |

> **Atenção:** tools que enviam SMS, executam shell, leem dados de CRM ou disparam builds agem em sistemas reais. Restrinja escopo e exija aprovação humana (capítulo 9, `HumanInTheLoop`).

## 5.9 Boas práticas

1. Uma tool, uma responsabilidade. Nomes em `snake_case`, verbos claros (`get_weather`, `create_ticket`).
2. Descreva cada campo com `description`; o modelo usa isso para preencher os argumentos.
3. Devolva resultados curtos e úteis. Resultados enormes consomem contexto; use `ContentAndArtifact` ou resuma.
4. Torne tools com efeito colateral idempotentes quando possível, pois um retry pode repetir a chamada.
5. Valide regras de negócio dentro da tool e responda com `tools.Errorf` para o modelo se corrigir.

Próximo: [6. RAG](06-rag.md).
