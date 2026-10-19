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

## `rag`: indexador e pipeline

### `Indexer`

```go
idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
stats, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil))
// stats.Added, stats.Skipped, stats.Removed (contados em chunks)
```

- Agrupa os documentos por `metadata["source"]` (documento sem `source` ganha um grupo próprio, por posição) e calcula o sha256 do conteúdo de cada grupo.
- Fonte inalterada é ignorada (`Skipped`). Fonte alterada tem os chunks antigos apagados do store (`Removed`) e é reindexada (`Added`).
- Cada chunk recebe `source`, `chunk` e `content_hash`; o splitter já informa `start_index`.
- `Documents()` devolve todos os chunks indexados, ordenados por fonte: é a entrada do lado BM25 do pipeline.
- Seguro para uso concorrente.

### `Pipeline`

```go
p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, MultiQueries: 2, Reranker: rr, MaxContextTokens: 3000,
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "..."})
```

| Campo | Efeito |
|---|---|
| `K` (4) | fontes devolvidas |
| `FetchK` | candidatos buscados antes do rerank e do corte em `K` |
| `Hybrid` | funde a busca vetorial com BM25 sobre `Docs` por RRF (`retrievers.Ensemble`) |
| `MultiQueries` | reformulações extras da pergunta pelo modelo (`retrievers.MultiQuery`) |
| `Reranker` | reordena os candidatos (`retrieval.WithRerank`, por exemplo o rerank da Cohere) |
| `MaxContextTokens` | corta fontes do fim até caber, medindo com `Counter` (`nil` = `knowledge.Heuristic`) |
| `System` | substitui `knowledge.DefaultSystem` |

- `Question.Filter` vale para a busca vetorial e para o BM25 (aplicado depois da pontuação).
- Sem fontes, o pipeline devolve resposta vazia **sem chamar o modelo**, e o `Trace` diz `generate: skipped: no sources`.
- `Retrieve(ctx, q)` devolve só as fontes, sem gerar. Recusa `Question.Force` diferente de `""` e `"rag"`.
- `Docs` é uma cópia do momento da criação: depois de reindexar, monte o `Pipeline` de novo ou atribua `Docs = idx.Documents()`.

### Limites

- O estado de hashes do `Indexer` fica só na memória do processo; reiniciar reindexa tudo. Fontes que somem do loader não são removidas do store.
- O índice BM25 é recriado a cada pergunta. Para corpus grandes e estáveis, isso deve virar cache.
- Embeddings não passam por `embeddings.Cached` automaticamente; embrulhe o embedder do store se o custo importar.

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

### Extração de triplas (`Extractor`)

```go
x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCREVEU", "INTERAGE_COM"}}}
stats, err := x.Extract(ctx, chunks) // ExtractStats{Entities, Relations, Dropped}
```

Para cada documento o modelo devolve, por saída estruturada, `{entities:[{name,type}], relations:[{from,type,to}]}`.

- IDs vêm de `NormalizeID(name)`. Relação com ponta que o modelo não listou cria a entidade.
- `Relation.Source` é o `ID` do documento ou o `metadata["source"]`, para rastrear de onde veio cada tripla.
- Vazios e duplicatas são descartados e contados em `Dropped`. Documento em branco é pulado sem chamar o modelo.
- `Embedder` é opcional: sem ele, as entidades não têm vetor e a busca cai para nome. Se o embedder falhar, nada é gravado e o erro volta.
- Um chunk com saída inválida não derruba o lote: os erros são juntados (`errors.Join`, a mensagem cita `chunk N`) e os chunks bons são gravados.
- `Schema.RelationTypes` filtra os tipos aceitos. `Schema.EntityTypes` só entra no prompt, não é imposto.

**Segurança.** O tipo de relação vem de um LLM e depois vira tipo de relacionamento em Cypher (Neo4j e Memgraph), onde não pode ser parâmetro.
O extrator normaliza todo tipo para `^[A-Z][A-Z0-9_]*$`: maiúsculas, qualquer outro caractere vira `_`, repetições colapsam, `_` das pontas sai,
e tipo vazio ou começado por dígito é descartado. `trata com; DROP` vira `TRATA_COM_DROP`. A normalização é só ASCII, então acentos viram `_`.
