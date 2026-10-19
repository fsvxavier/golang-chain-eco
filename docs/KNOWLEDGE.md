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

## `cag`: corpus em prefixo cacheável

O CAG coloca o corpus inteiro na mensagem `system` e deixa o cache de prompt do provedor fazer o resto. Não há busca em tempo de execução.

### `Corpus`

- `cag.NewCorpus(docs)` ordena os documentos por `metadata["source"]`, depois por `ID` e por conteúdo, então a ordem de entrada não muda o prefixo.
- `Text()` monta blocos `<document source="...">...</document>`; `Hash()` é o sha256 desse texto; `Tokens(counter)` estima o tamanho com um `knowledge.TokenCounter`
  (`nil` usa `knowledge.Heuristic`); `Documents()` devolve os documentos já ordenados.
- Os métodos aceitam `*Corpus` nulo.

### `Engine`

```go
e := &cag.Engine{
    Model:    model,
    Corpus:   cag.NewCorpus(docs),
    Window:   200000, // janela do modelo em tokens (padrão)
    Reserve:  4096,   // folga para histórico, pergunta e resposta (padrão)
    Fallback: ragPipeline, // opcional: qualquer knowledge.Answerer
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "..."})
```

Ordem das mensagens: `system` (instrução + corpus), depois `Question.History`, depois a pergunta. O histórico nunca vem antes do corpus, senão o prefixo mudaria a cada turno e o cache não acertaria.

| Situação | Resultado |
|---|---|
| corpus + `Reserve` cabe em `Window` | resposta com `Strategy == "cag"`; `Trace` traz `corpus_hash` |
| resposta traz `Usage.CacheReadTokens > 0` | `Trace` ganha o passo `cache_hit` |
| não cabe e há `Fallback` | o `Fallback` responde; `Trace` começa com o passo `fallback` e o motivo |
| não cabe e não há `Fallback` | `cag.ErrCorpusTooLarge` |
| corpus vazio | `cag.ErrEmptyCorpus` |
| pergunta vazia | `knowledge.ErrEmptyQuestion` |

`Engine.SetCorpus(c)` troca o corpus e devolve `true` quando o hash mudou, ou seja, quando o cache do provedor será reconstruído.
É seguro chamar junto com `Answer`.

### Marcação do prefixo (`PrefixCacher`)

| Implementação | Efeito | Usada para |
|---|---|---|
| `cag.AnthropicCache` | envia o system como um bloco com `cache_control: {type: ephemeral}` | modelos `anthropic:*` |
| `cag.StablePrefix` | não altera a mensagem; só garante prefixo idêntico byte a byte | OpenAI e compatíveis (cache automático por prefixo) |

`cag.CacherFor(modelID)` escolhe pelo prefixo do id do modelo; `Engine.Cacher` sobrescreve. Provedores sem cache de prompt funcionam,
só não economizam: a economia aparece em `Usage.CacheReadTokens`.

### Limites

- A contagem de tokens é a heurística de `knowledge.Heuristic` (~4 caracteres por token). Corpus perto da janela merece um contador próprio.
- O CAG não tem estado de cache local: depende do cache do provedor, que expira (minutos no Anthropic) e cobra a primeira escrita.
