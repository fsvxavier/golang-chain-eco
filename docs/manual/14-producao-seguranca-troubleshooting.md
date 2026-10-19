# 14. Produção, segurança e troubleshooting

## 14.1 Checklist de produção

**Persistência**

- [ ] Checkpointer durável (`sqlsaver`, Redis, MongoDB, DynamoDB, S3), nunca `checkpoint.NewMemory()`.
- [ ] `thread_id` derivado do usuário/sessão autenticada, não aceito cru do cliente (evita ler a conversa de outro).
- [ ] Tipos próprios do estado registrados com `checkpoint.Register`.
- [ ] Política de retenção: `saver.DeleteThread(ctx, id)` para apagar conversas antigas.

**Custo e estabilidade**

- [ ] `ModelCallLimit` e `ToolCallLimit` no agent.
- [ ] `RecursionLimit` explícito em `runnables.Config`.
- [ ] `ModelRetry`/`ToolRetry` e `ModelFallback` para erros transitórios.
- [ ] `Summarization` ou `ContextEditing` para conversas longas.
- [ ] `models.NewRateLimiter` por provedor, respeitando as cotas da conta.
- [ ] `context.WithTimeout` em toda chamada vinda de uma requisição HTTP.

**Observabilidade**

- [ ] Um tracer ativo (LangSmith, Langfuse ou OTLP) com `defer Close()`.
- [ ] `OnError` do tracer ligado a um log.
- [ ] Uso de tokens e latência por modelo/tool em métricas.

**Segurança**

- [ ] Segredos em variáveis de ambiente ou cofre.
- [ ] Tools sensíveis protegidas por `HumanInTheLoop`.
- [ ] `PII` ativo quando houver dados pessoais.
- [ ] Sem `LocalShell` em produção; usar `kubernetes.Sandbox` ou runner isolado.
- [ ] `Permissions` configuradas nos Deep Agents com disco real.

## 14.2 Encerramento e contexto

Todo método recebe `context.Context`. Cancelar o contexto interrompe a chamada ao provedor, o streaming e o grafo. Em servidores HTTP, propague `r.Context()`:

```go
func handler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = runnables.WithConfig(ctx, graph.Thread(userThread(r)))
	out, err := agent.Invoke(ctx, graph.State{"messages": readBody(r)})
	...
}
```

No encerramento do processo: pare de aceitar requisições, espere as em andamento, depois chame `Close()` dos tracers e feche conexões.

## 14.3 Concorrência

- `*graph.Compiled`, modelos e tools são seguros para uso concorrente e devem ser **criados uma vez** e reaproveitados entre requisições.
- Nós de um grafo rodam em goroutines: não compartilhe mapas ou slices mutáveis entre nós sem sincronização. O estado entregue ao nó é uma cópia.
- `callbacks.Handler` deve ser seguro para concorrência.
- Cada thread (`thread_id`) deve ter **uma execução por vez**. Duas chamadas simultâneas na mesma thread competem pelo mesmo checkpoint; serialize no seu código (fila por thread) ou use `Multitask` no servidor remoto.

## 14.4 Segurança

### Execução de código

| Componente | Risco | Mitigação |
|---|---|---|
| `backends.LocalShell` | Executa comandos no **host**, sem sandbox | Só em ambiente confiável; `AllowedCommands` restringe os programas; em produção use `kubernetes.Sandbox` ou outro runner isolado via `backends.Remote` |
| `mcp.Stdio` | Inicia processo com suas permissões | Só servidores confiáveis; fixe versões; restrinja `Env` |
| `backends.Filesystem` | Acesso ao disco | Confinado à `Root` (bloqueia `..` e symlinks que escapam); defina `Permissions` |

### Prompt injection

Conteúdo vindo de páginas web, documentos, resultados de tools ou e-mails pode conter instruções maliciosas. Princípios:

1. Dê ao agent apenas as tools de que ele precisa.
2. Exija aprovação humana (`HumanInTheLoop`) para ações irreversíveis ou com efeito externo (enviar, pagar, apagar, executar).
3. Restrinja destinos e escopos (`messaging.Allow`, SOQL somente leitura, namespaces do Kubernetes).
4. Aplique permissões **na camada de dados** (`spicedb.FilterRetriever`, `permit.FilterRetriever`), não confiando no prompt para "não revelar".
5. Trate a saída do modelo como entrada não confiável antes de usá-la em SQL, shell ou HTML.

### SSRF

`search.Fetcher` recusa loopback e redes privadas por padrão, mesmo após resolver o DNS. Não ligue `AllowPrivate` em código exposto a entrada de usuário.

### Segredos

- Não registre `Authorization`, chaves de API ou o corpo de prompts com dados pessoais em logs.
- Chaves de tracing e provedores via ambiente. `remote/server` usa `APIKey` compartilhada: coloque TLS e autenticação real na frente.

## 14.5 Desempenho

- Reaproveite o `http.Client` (`HTTP` nas opções dos provedores e tracers) para keep-alive.
- `models.WithCache` evita repetir chamadas idênticas (útil em testes e avaliações).
- `embeddings.NewCached` evita reembutir os mesmos textos.
- Use `Parallel`, ramos paralelos e `Send` para I/O independente (várias buscas, vários documentos).
- Para volume de vetores, troque `vectorstores.Memory` (busca linear) por um backend com índice.
- Streaming (`ModeMessages`) melhora a latência percebida em chats.

## 14.6 Testes

```bash
go test -race ./...                                 # tudo; testes de serviços reais se pulam sozinhos (sem rede)
go test ./internal/sqlitetest/...                   # sqlsaver contra SQLite real (driver modernc.org/sqlite)
PG_DSN=postgres://... go test ./internal/pgtest/... # PostgreSQL, MariaDB, CockroachDB, Oracle
```

Os testes que precisam de serviços reais **se pulam sozinhos** quando a variável de ambiente não está definida (`REDIS_ADDR`, `MONGO_ADDR`, `DYNAMODB_ENDPOINT`, `S3_ENDPOINT`, `SOLR_URL`, `CLICKHOUSE_URL`, `K8S_SERVER`...). Para subir os serviços, veja [INTEGRATIONS.md](../INTEGRATIONS.md#testes-contra-serviços-reais).

Para os **seus** testes:

- `models.NewFake(...)` com respostas roteirizadas; confira `fake.Calls`.
- `embeddings.Fake{Dim: 64}` para RAG determinístico.
- `httptest.NewServer` para simular provedores (use `BaseURL`).
- Teste o fluxo de HITL: invoque, verifique `graph.Interrupts(out)`, retome com `graph.Command{Resume: ...}`.

## 14.7 Troubleshooting

| Sintoma | Causa provável | Solução |
|---|---|---|
| `Init` devolve erro de provedor/chave | Variável `<PROVEDOR>_API_KEY` ausente ou prefixo errado | Confira `chatmodels.Providers()` e as variáveis do capítulo 1 |
| Erro pedindo `<PROVEDOR>_BASE_URL` | Provedor por tenant (Databricks, Snowflake, gateways) | Defina a variável com o endpoint |
| `*graph.GraphRecursionError` | Loop passou do limite (padrão 25) | Aumente `RecursionLimit` se legítimo; senão corrija a condição de saída |
| O agent não lembra a conversa | Sem checkpointer ou sem `graph.Thread(id)` no contexto | `Compile: graph.WithCheckpointer(...)` e `runnables.WithConfig(ctx, graph.Thread(id))` |
| `Interrupt` não pausa / não retoma | Sem checkpointer, ou `thread_id` diferente na retomada | Use o mesmo `thread_id`; envie `graph.Command{Resume: ...}` |
| Efeito colateral executado duas vezes após retomar | O nó reexecuta desde o início | Mova a ação para depois do `Interrupt` ou torne-a idempotente |
| Estado volta com `float64` ou `map[string]any` em vez do tipo próprio | Serialização JSON do checkpoint | `checkpoint.Register("nome", Tipo{})` |
| Número volta como `float64` (ou `int`) em vez do tipo esperado | Números sem tipo explícito voltam como `int` ou `float64`; escalares Go como `int64` e `float32` já são preservados | Use o tipo escalar no estado, ou `checkpoint.Register` para um tipo próprio; `checkpoint.Strict(true)` acusa structs não registrados |
| Escritas de ramos paralelos se perdem/conflitam | Canal sem reducer | Use `graph.Append()`, `graph.Sum()` ou `channels.BinOp` |
| Tool nunca é chamada | Descrição vaga, schema confuso ou modelo sem suporte a tools | Melhore nome/descrição/`description` dos campos; teste com outro modelo |
| Tool chamada com argumentos inválidos | Schema permissivo | Use `jsonschema:"enum=..."`, `minimum`, `required`; devolva `tools.Errorf` explicando |
| Resposta em um só bloco no Bedrock | Stream binário não decodificado | Esperado: `Stream` devolve a resposta completa |
| `human_in_the_loop` falha ao pausar | Sem checkpointer | Passe `Compile: []graph.CompileOption{graph.WithCheckpointer(s)}` |
| `execute` some no Deep Agent | Existe regra `Deny` em `Permissions` | Comportamento intencional: shell contornaria as regras |
| Vector store devolve vazio | Embedder diferente na ingestão e na consulta, ou índice ainda não criado | Use o mesmo embedder/dimensão; crie o índice antes (Pinecone, Atlas) |
| Eventos de tracing não aparecem | Faltou `Close()` ou fila cheia | `defer tr.Close()`; configure `OnError` e aumente `Buffer` |
| `mongodb+srv://` não funciona | Não suportado | Passe `host:porta` |
| Rate limit 429 do provedor | Cotas | `models.NewRateLimiter`, `ModelRetry` com backoff, `ModelFallback` |
| Contexto estoura o limite do modelo | Histórico longo | `Summarization`, `ContextEditing` ou `messages.Trim` |

## 14.8 Limitações conhecidas

- O estado é `map[string]any`, não TypedDict/Pydantic. Use `tools.Decode` para ligar em structs.
- No `sqlsaver`, escalares Go (`int64`, `float32`, `time.Time`, `[]byte`…) voltam com o tipo original; números sem tipo explícito voltam como `int` ou `float64`; tipos próprios exigem `checkpoint.Register`.
- Em `graph.Interrupt`, o nó reexecuta desde o início. Subgrafos com checkpointer próprio não propagam interrupts ao pai.
- Não portado da versão Python: modos de durabilidade, Studio/CLI, backends gerenciados do LangSmith e avaliações (evals).
- Sem parser de PDF nativo (use `Unstructured` ou converta antes).
- `Flat` e `Annoy` são equivalentes em Go puro a FAISS/Annoy. `LoadAnnoyFile` e `LoadFAISSFlat` leem os formatos nativos (Annoy angular/euclidiano; FAISS flat e IDMap sobre flat), mas a compatibilidade com arquivos gerados pelas bibliotecas oficiais ainda não foi verificada por teste (ver [INTEGRATIONS.md](../INTEGRATIONS.md)).
- Integrações de SaaS/API paga só têm teste de contrato; valide no seu ambiente.
- 123 provedores da lista da LangChain dependem de documentação de API indisponível ([PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md)).

## 14.9 Evolução e compatibilidade

O módulo exige Go 1.27+ e o código de produção não importa dependências de terceiros (os drivers do `go.mod` são só de teste), então atualizar é trocar a versão em `go.mod` (`go get github.com/fsvxavier/golang-chain-eco@latest`) e rodar `go build ./... && go test -race ./...`. Leia o histórico do repositório antes de atualizações maiores, pois a API acompanha o ecossistema Python, que evolui rapidamente.

Próximo: [15. Exemplos e receitas](15-exemplos-e-receitas.md).
