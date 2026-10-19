# 8. Persistência, human-in-the-loop e streaming

## 8.1 Checkpointers e threads

Com um checkpointer, cada super-step é salvo por `thread_id`. Isso permite continuar conversas, pausar para aprovação humana, inspecionar o histórico e voltar no tempo.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("usuario-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "oi"})
out, _ = app.Invoke(ctx, graph.State{"messages": "e agora?"}) // continua a mesma conversa
```

- Sem `thread_id` na configuração, o grafo não persiste entre chamadas.
- Cada thread tem sua própria linha do tempo de checkpoints.
- A mesma regra vale para `agents.Create` (via `Options.Compile`) e `deepagents.Create` (via `Options.Checkpointer`).

### Durabilidade

Por padrão (`graph.DurabilitySync`) o checkpoint é gravado antes de cada super-step seguir. Duas alternativas trocam garantia por latência:

| Modo | Quando grava | Uso |
|------|--------------|-----|
| `DurabilitySync` | A cada super-step, antes de continuar (padrão) | Máxima garantia: uma queda perde no máximo o passo em curso |
| `DurabilityAsync` | A cada super-step, em uma goroutine de fundo, em ordem | Reduz a latência; `Invoke`/`Stream` só retornam depois do último save. Um erro de save aborta a execução no próximo super-step |
| `DurabilityExit` | Só ao pausar (interrupt), terminar ou falhar entre super-steps | Menos escritas; uma queda no meio da execução perde os passos desde o último checkpoint |

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.WithDurability(graph.DurabilityAsync)) // padrão do grafo

// por chamada, sobrescreve o padrão do grafo
ctx := graph.WithRunDurability(runnables.WithConfig(context.Background(), graph.Thread("usuario-42")), graph.DurabilityExit)
out, err := app.Invoke(ctx, graph.State{"messages": "oi"})
```

- Pausas de subgrafos com checkpointer próprio são sempre gravadas, em qualquer modo, porque o pai precisa retomar a partir delas.
- Em `exit`, uma falha de nó entre super-steps grava o último estado bom, então a thread pode ser retomada. Uma falha de redutor no meio de um super-step não grava nada.

### Backends de checkpoint

| Backend | Construtor | Observação |
|---|---|---|
| Memória | `checkpoint.NewMemory()` | Testes e protótipos; perde tudo ao encerrar |
| SQLite / PostgreSQL | `sqlsaver.New(ctx, db, sqlsaver.SQLite \| sqlsaver.Postgres)` | `database/sql`; você traz o driver |
| Redis | `redis.NewSaver(redis.New("host:6379"))` | Cliente RESP próprio |
| MongoDB / Atlas | `mongo.NewSaver(client)` | OP_MSG próprio; sem `mongodb+srv://` |
| DynamoDB | `dynamodb.NewSaver(ctx, client, "tabela")` | Cria tabela on-demand; item ≤ 400 KB |
| S3 e compatíveis | `s3.NewSaver(client, "bucket", "prefixo")` | AWS, MinIO, Tigris, R2... |

Exemplo com SQLite:

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // ou _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // ou sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

### Serialização: tipos próprios

Os valores são gravados em JSON. Se o estado contém tipos seus, registre-os para sobreviverem ao round trip:

```go
checkpoint.Register("meuapp.Pedido", Pedido{})
checkpoint.Register("[]meuapp.Pedido", []Pedido{})
```

Já vêm registrados: mensagens, `graph.Send`, arquivos, todos, tasks assíncronas e avaliações de rubrica, além dos escalares Go `int8`…`int64`, `uint`…`uint64`, `float32`, `[]byte`, `time.Time` e `time.Duration`, que voltam com o tipo original. Números sem tipo explícito continuam como `int` (inteiros) ou `float64`. Com `checkpoint.Strict(true)`, gravar um struct não registrado passa a ser erro em vez de virar `map[string]any`.

### Backend próprio

Implemente `checkpoint.Saver` (4 métodos):

```go
type Saver interface {
	Put(ctx context.Context, cp Checkpoint) (string, error)
	Get(ctx context.Context, cfg Config) (Checkpoint, bool, error)
	List(ctx context.Context, threadID string, limit int) ([]Checkpoint, error)
	DeleteThread(ctx context.Context, threadID string) error
}
```

Valide-o com a suíte `internal/conformance` (a mesma usada pelos backends do projeto).

## 8.2 Human-in-the-loop com `Interrupt`

`graph.Interrupt(ctx, valor)` pausa a execução e devolve o valor ao chamador. Na retomada, `Interrupt` retorna a resposta enviada.

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// A primeira execução pausa aqui. Na retomada, Interrupt devolve o valor do resume.
	answer := graph.Interrupt(ctx, map[string]any{"pergunta": "Aprovar o envio?"})
	if answer != "sim" {
		return graph.State{"status": "cancelado"}, nil
	}
	return graph.State{"status": "enviado"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "novo"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("pausado em", i.Node, i.Value) // InterruptInfo{ID, Node, Value}
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "sim"}) // retoma
```

Requisitos e regras:

- **Exige checkpointer e `thread_id`**: o estado pausado fica salvo.
- **O nó reexecuta desde o início na retomada.** Mantenha idempotente (ou mova para depois do `Interrupt`) tudo que tem efeito colateral e vem antes dele.
- Vários interrupts simultâneos (ramos paralelos): responda por ID com `graph.Command{ResumeMap: map[string]any{id: valor}}`.
- Tasks irmãs já concluídas no mesmo passo não são reexecutadas.

### Pausas estáticas

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.InterruptBefore("tools"))
// ou graph.InterruptAfter("planner")

out, _ := app.Invoke(ctx, input) // roda até antes de "tools" e pausa
// inspecione/edite o estado...
out, _ = app.Invoke(ctx, nil)    // nil continua de onde parou
```

## 8.3 Inspeção e time travel

```go
snap, _ := app.GetState(ctx)            // Values, Next, Interrupts, CheckpointID, ParentID, Step
hist, _ := app.GetStateHistory(ctx, 10) // do mais recente ao mais antigo

// edita o estado como se o nó "research" tivesse escrito:
app.UpdateState(ctx, graph.State{"topic": "outro"}, "research")

// fork a partir de um checkpoint antigo:
old := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID},
})
app.Invoke(old, nil)
```

Usos: depurar uma execução passo a passo, corrigir manualmente um estado ruim e continuar, ou explorar alternativas a partir de um ponto anterior.

## 8.4 Memória de longo prazo (`langgraph/store`)

O checkpoint guarda o estado **de uma thread**. O `store.Store` guarda conhecimento **entre threads** (preferências, fatos sobre o usuário).

```go
type Store interface {
	Put(ctx, ns []string, key string, value map[string]any) error
	Get(ctx, ns []string, key string) (*Item, error)
	Delete(ctx, ns []string, key string) error
	Search(ctx, prefix []string, filter map[string]any, limit int) ([]Item, error)
	ListNamespaces(ctx, prefix []string) ([][]string, error)
}
```

| Backend | Construtor |
|---|---|
| Memória | `store.NewMemory()` |
| SQLite / PostgreSQL | `sqlstore` (`langgraph/store/sqlstore`) |
| Redis | `redis.NewStore(client)` |
| MongoDB | `mongo.NewStore(client)` |
| DynamoDB | `dynamodb.NewStore(ctx, client, "tabela")` (`Search` faz Scan) |
| S3 | `s3.NewStore(client, "bucket", "prefixo")` |

Plugue no grafo com `graph.WithStore(s)`; no nó, `graph.StoreFrom(ctx)`. Nos Deep Agents, `backends.Store` expõe um `Store` como sistema de arquivos compartilhado.

## 8.5 Streaming

`app.Stream(ctx, input)` devolve o estado a cada passo. `StreamEvents` permite escolher os modos:

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // ev.Node + atualização, a cada nó concluído
	case graph.ModeValues:   // estado completo a cada passo
	case graph.ModeMessages: // tokens do LLM conforme chegam (ev.Data é messages.Message)
	case graph.ModeCustom:   // o que o nó enviou via graph.StreamWriter(ctx)(valor)
	}
}
```

Evento customizado, dentro de um nó:

```go
write := graph.StreamWriter(ctx)
write(map[string]any{"progresso": 40})
```

Quando usar cada modo:

| Modo | Uso típico |
|---|---|
| `ModeValues` | Depuração; UI que redesenha o estado inteiro |
| `ModeUpdates` | Mostrar "o que cada etapa fez" |
| `ModeMessages` | Chat com digitação em tempo real |
| `ModeCustom` | Barras de progresso e telemetria de negócio |

A iteração é um `iter.Seq2`. Se você sair do `for` mais cedo (`break`), a execução é cancelada. Para servir esses eventos a um navegador, veja o protocolo AG-UI no [capítulo 11](11-mcp-remote-servidor.md#115-protocolo-ag-ui).

## 8.6 Receita: chatbot que lembra e pede aprovação

```go
saver := checkpoint.NewMemory() // troque por sqlsaver/redis em produção
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{search, sendEmail},
	Middleware: []agents.Middleware{
		middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
	},
	Compile: []graph.CompileOption{graph.WithCheckpointer(saver)},
})

ctx := runnables.WithConfig(ctx, graph.Thread("conv-1"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Envie o resumo para o Bob"})
if reqs := graph.Interrupts(out); len(reqs) > 0 {
	// mostre reqs[0].Value ao usuário; depois:
	out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
		Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	}})
}
```

Próximo: [9. Agents e middleware](09-agents-e-middleware.md).
