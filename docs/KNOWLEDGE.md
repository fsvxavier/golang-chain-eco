# Knowledge: RAG, CAG e KAG

Referência dos pacotes `knowledge`, `rag`, `cag` e `kag`. Desenho completo em [DESIGN-RAG-CAG-KAG.md](../DESIGN-RAG-CAG-KAG.md).
O código de biblioteca usa só a stdlib; modelo, embedder, vector store e grafo entram por interface.

## `knowledge`: contrato comum

Toda técnica implementa `knowledge.Answerer`:

```go
type Answerer interface {
    Answer(ctx context.Context, q Question) (Answer, error)
}
```

| Tipo | Campos | Uso |
|---|---|---|
| `Question` | `Text`, `History []messages.Message`, `Filter vectorstores.Filter`, `Force string` | `Force` vale `""` (automático), `"rag"`, `"cag"` ou `"kag"` |
| `Answer` | `Text`, `Sources []Source`, `Strategy`, `Usage *messages.Usage`, `Trace []Step` | `Strategy` diz quem respondeu; `Trace` registra as decisões |
| `Source` | `ID`, `Content`, `Metadata`, `Score` | evidência usada na resposta |

Funções auxiliares:

- `knowledge.AsRunnable(a)` expõe um `Answerer` como `runnables.Runnable[Question, Answer]`.
- `knowledge.SourcesFromDocs(docs)` converte documentos recuperados em `Source` (o `Score` vem de `metadata["score"]` quando é `float64`).
- `knowledge.Prompt(system, fontes, q)` monta as mensagens: system com o contexto numerado (`[1]`, `[2]`...), histórico e a pergunta.
  `knowledge.DefaultSystem` pede resposta só com base no contexto e citação `[n]`. `Prompt` não troca `system` vazio por `DefaultSystem`.
- `knowledge.ErrEmptyQuestion` é devolvido para pergunta sem texto.

### Contagem de tokens

`knowledge.TokenCounter` (`Count(text string) int`) é substituível. O padrão `knowledge.Heuristic{CharsPerToken: 4}` conta runas
e arredonda para cima (`""` dá 0). É uma estimativa, não um tokenizer: use um contador próprio quando a margem importar.

## Métricas de cache em `messages.Usage`

`messages.Usage` tem dois campos novos, aditivos (`omitempty` no JSON):

| Campo | Significado |
|---|---|
| `CacheReadTokens` | tokens de entrada lidos do cache de prompt |
| `CacheCreationTokens` | tokens de entrada gravados no cache (só o Anthropic informa) |

`InputTokens` continua somando tudo, inclusive os tokens de cache, então código existente não muda. `Usage.Add` soma os dois campos.
Preenchimento por provedor:

- Anthropic: `cache_read_input_tokens` e `cache_creation_input_tokens`.
- OpenAI e compatíveis: `prompt_tokens_details.cached_tokens` vai para `CacheReadTokens`.
- Os demais provedores deixam os campos em zero.

## `kag`: grafo de conhecimento

### Modelo

| Tipo | Campos |
|---|---|
| `Entity` | `ID`, `Type`, `Name`, `Props map[string]any`, `Vector []float32` (opcional, habilita busca por cosseno) |
| `Relation` | `From`, `Type`, `To`, `Props`, `Source` (chunk ou documento de origem) |
| `Direction` | `Out`, `In`, `Both` |

`kag.NormalizeID(s)` põe em minúsculas, apara e troca espaços por `_`.

### `GraphStore`

```go
type GraphStore interface {
    UpsertEntities(ctx, []Entity) error
    UpsertRelations(ctx, []Relation) error
    Neighbors(ctx, id string, relTypes []string, dir Direction) ([]Relation, error)
    FindEntities(ctx, text string, vec []float32, k int) ([]Entity, error)
    Entity(ctx, id string) (Entity, bool, error)
    Path(ctx, from, to string, maxHops int) ([][]Relation, error)
    Delete(ctx, ids []string) error
}
```

`kag.NewMemory()` é a implementação de referência, segura para uso concorrente. `Save(path)` e `kag.LoadMemory(path)` persistem em JSON.
Comportamento que qualquer adapter deve repetir:

- `UpsertRelations` com o mesmo `(From, Type, To)` substitui `Props` e `Source`.
- `Path` segue só arestas de saída e devolve os caminhos **mais curtos**; devolve `nil` se `from == to` ou `maxHops <= 0`. Termina em grafos com ciclo.
- `Delete` remove as entidades e todas as relações que as tocam.
- `FindEntities` usa cosseno quando `vec` e `Entity.Vector` existem; senão, a fração dos termos da consulta presentes em `Name` ou `ID`. Score 0 fica de fora e o empate sai por `ID`.
- `Neighbors` devolve em ordem estável (`From|Type|To`).

A suíte `conformance.Graph(t, store)` (pacote `internal/conformance`) verifica esse contrato e deve rodar sobre todo adapter novo.
