# Knowledge: RAG, CAG and KAG

**English** | [Português](../pt-BR/KNOWLEDGE.md) | [Español](../es-ES/KNOWLEDGE.md)

Reference for the `knowledge`, `rag`, `cag` and `kag` packages.
The library code uses only the standard library; the model, embedder, vector store and graph come in through interfaces.

## Contents

- [`knowledge`: common contract](#knowledge-common-contract) and [cache metrics](#cache-metrics-in-messagesusage)
- [`rag`: indexer and pipeline](#rag-indexer-and-pipeline)
- [`cag`: corpus in a cacheable prefix](#cag-corpus-in-a-cacheable-prefix)
- [`kag`: knowledge graph](#kag-knowledge-graph): model, `GraphStore`, triple extraction, `Engine`
- [`knowledge.Router`](#knowledgerouter-choosing-the-technique)
- [`knowledge.Toolkit`: tools and MCP](#knowledgetoolkit-tools-for-agents-and-mcp-server)
- [`integrations/graphstore`: Neo4j and Memgraph](#integrationsgraphstore-graph-on-neo4j-and-memgraph)
- [Which database to combine with each technique](#which-database-to-combine-with-each-technique)
- [`cmd/knowledge`: command line](#cmdknowledge-command-line)

For the step-by-step guide with recipes, see [chapter 20 of the manual](manual/20-rag-cag-kag.md).

## `knowledge`: common contract

Every technique implements `knowledge.Answerer`:

```go
type Answerer interface {
    Answer(ctx context.Context, q Question) (Answer, error)
}
```

| Type | Fields | Use |
|---|---|---|
| `Question` | `Text`, `History []messages.Message`, `Filter vectorstores.Filter`, `Force string` | `Force` is `""` (automatic), `"rag"`, `"cag"` or `"kag"` |
| `Answer` | `Text`, `Sources []Source`, `Strategy`, `Usage *messages.Usage`, `Trace []Step` | `Strategy` says who answered; `Trace` records the decisions |
| `Source` | `ID`, `Content`, `Metadata`, `Score` | evidence used in the answer |

Helper functions:

- `knowledge.AsRunnable(a)` exposes an `Answerer` as a `runnables.Runnable[Question, Answer]`.
- `knowledge.SourcesFromDocs(docs)` converts retrieved documents into `Source` (the `Score` comes from `metadata["score"]` when it is a `float64`).
- `knowledge.Prompt(system, sources, q)` builds the messages: system with the numbered context (`[1]`, `[2]`...), the history and the question.
  `knowledge.DefaultSystem` asks for an answer based only on the context and a `[n]` citation. `Prompt` does not swap an empty `system` for `DefaultSystem`.
- `knowledge.ErrEmptyQuestion` is returned for a question with no text.

### Token counting

`knowledge.TokenCounter` (`Count(text string) int`) is replaceable. The default `knowledge.Heuristic{CharsPerToken: 4}` counts runes
and rounds up (`""` gives 0). It is an estimate, not a tokenizer: use your own counter when the margin matters.

## Cache metrics in `messages.Usage`

`messages.Usage` has two new, additive fields (`omitempty` in JSON):

| Field | Meaning |
|---|---|
| `CacheReadTokens` | input tokens read from the prompt cache |
| `CacheCreationTokens` | input tokens written to the cache (only Anthropic reports it) |

`InputTokens` still sums everything, including cache tokens, so existing code does not change. `Usage.Add` sums both fields.
Filling per provider:

- Anthropic: `cache_read_input_tokens` and `cache_creation_input_tokens`.
- OpenAI and compatible: `prompt_tokens_details.cached_tokens` goes to `CacheReadTokens`.
- The other providers leave the fields at zero.

## `rag`: indexer and pipeline

### `Indexer`

```go
idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
stats, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil))
// stats.Added, stats.Skipped, stats.Removed (counted in chunks)
```

- Groups documents by `metadata["source"]` (a document without `source` gets its own group, by position) and computes the sha256 of each group's content.
- An unchanged source is skipped (`Skipped`). A changed source has its old chunks deleted from the store (`Removed`) and is reindexed (`Added`).
- Each chunk gets `source`, `chunk` and `content_hash`; the splitter already reports `start_index`.
- `Documents()` returns all indexed chunks, sorted by source: it is the input of the BM25 side of the pipeline.
- Safe for concurrent use.

### `Pipeline`

```go
p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, MultiQueries: 2, Reranker: rr, MaxContextTokens: 3000,
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "..."})
```

| Field | Effect |
|---|---|
| `K` (4) | sources returned |
| `FetchK` | candidates fetched before reranking and the cut to `K` |
| `Hybrid` | fuses vector search with BM25 over `Docs` by RRF (`retrievers.Ensemble`) |
| `MultiQueries` | extra rewordings of the question by the model (`retrievers.MultiQuery`) |
| `Reranker` | reorders the candidates (`retrieval.WithRerank`, for example Cohere's rerank) |
| `MaxContextTokens` | trims sources from the end until they fit, measuring with `Counter` (`nil` = `knowledge.Heuristic`) |
| `System` | replaces `knowledge.DefaultSystem` |

- `Question.Filter` applies to both the vector search and BM25 (applied after scoring).
- With no sources, the pipeline returns an empty answer **without calling the model**, and the `Trace` says `generate: skipped: no sources`.
- `Retrieve(ctx, q)` returns only the sources, without generating. It refuses a `Question.Force` other than `""` and `"rag"`.
- `Docs` is a copy taken at creation time: after reindexing, build the `Pipeline` again or assign `Docs = idx.Documents()`.

### Limits

- The `Indexer`'s hash state lives only in the process memory; restarting reindexes everything. Sources that disappear from the loader are not removed from the store.
- The BM25 index is rebuilt on every question. For large, stable corpora, this should become a cache.
- Embeddings do not go through `embeddings.Cached` automatically; wrap the store's embedder if the cost matters.

## `cag`: corpus in a cacheable prefix

CAG puts the whole corpus in the `system` message and lets the provider's prompt cache do the rest. There is no search at runtime.

### `Corpus`

- `cag.NewCorpus(docs)` sorts the documents by `metadata["source"]`, then by `ID` and by content, so the input order does not change the prefix.
- `Text()` builds `<document source="...">...</document>` blocks; `Hash()` is the sha256 of that text; `Tokens(counter)` estimates the size with a `knowledge.TokenCounter`
  (`nil` uses `knowledge.Heuristic`); `Documents()` returns the documents already sorted.
- The methods accept a nil `*Corpus`.

### `Engine`

```go
e := &cag.Engine{
    Model:    model,
    Corpus:   cag.NewCorpus(docs),
    Window:   200000, // model window in tokens (default)
    Reserve:  4096,   // room for history, question and answer (default)
    Fallback: ragPipeline, // optional: any knowledge.Answerer
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "..."})
```

Message order: `system` (instruction + corpus), then `Question.History`, then the question. The history never comes before the corpus, otherwise the prefix would change on every turn and the cache would never hit.

| Situation | Result |
|---|---|
| corpus + `Reserve` fit in `Window` | answer with `Strategy == "cag"`; `Trace` carries `corpus_hash` |
| response has `Usage.CacheReadTokens > 0` | `Trace` gains the `cache_hit` step |
| does not fit and there is a `Fallback` | the `Fallback` answers; `Trace` starts with the `fallback` step and the reason |
| does not fit and there is no `Fallback` | `cag.ErrCorpusTooLarge` |
| empty corpus | `cag.ErrEmptyCorpus` |
| empty question | `knowledge.ErrEmptyQuestion` |

`Engine.SetCorpus(c)` replaces the corpus and returns `true` when the hash changed, that is, when the provider's cache will be rebuilt.
It is safe to call alongside `Answer`.

### Prefix marking (`PrefixCacher`)

| Implementation | Effect | Used for |
|---|---|---|
| `cag.AnthropicCache` | sends the system as a block with `cache_control: {type: ephemeral}` | `anthropic:*` models |
| `cag.StablePrefix` | does not change the message; only guarantees a byte-for-byte identical prefix | OpenAI and compatible (automatic prefix cache) |

`cag.CacherFor(modelID)` chooses by the prefix of the model id; `Engine.Cacher` overrides it. Providers without a prompt cache work,
they just do not save anything: the saving shows up in `Usage.CacheReadTokens`.

### Limits

- The token count is the heuristic of `knowledge.Heuristic` (~4 characters per token). A corpus close to the window deserves its own counter.
- CAG has no local cache state: it depends on the provider's cache, which expires (minutes on Anthropic) and charges for the first write.

## `kag`: knowledge graph

### Model

| Type | Fields |
|---|---|
| `Entity` | `ID`, `Type`, `Name`, `Props map[string]any`, `Vector []float32` (optional, enables cosine search) |
| `Relation` | `From`, `Type`, `To`, `Props`, `Source` (origin chunk or document) |
| `Direction` | `Out`, `In`, `Both` |

`kag.NormalizeID(s)` lowercases, trims and replaces spaces with `_`.

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

`kag.NewMemory()` is the reference implementation, safe for concurrent use. `Save(path)` and `kag.LoadMemory(path)` persist to JSON.
Behavior every adapter must repeat:

- `UpsertRelations` with the same `(From, Type, To)` replaces `Props` and `Source`.
- `Path` follows only outgoing edges and returns the **shortest** paths; it returns `nil` if `from == to` or `maxHops <= 0`. It terminates on graphs with cycles.
- `Delete` removes the entities and every relation that touches them.
- `FindEntities` uses cosine when both `vec` and `Entity.Vector` exist; otherwise, the fraction of the query terms present in `Name` or `ID`. Score 0 is left out and ties are sorted by `ID`.
- `Neighbors` returns in a stable order (`From|Type|To`).

The `conformance.Graph(t, store)` suite (package `internal/conformance`) verifies this contract and must run against every new adapter.

### Triple extraction (`Extractor`)

```go
x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCRIBED", "INTERACTS_WITH"}}}
stats, err := x.Extract(ctx, chunks) // ExtractStats{Entities, Relations, Dropped}
```

For each document, the model returns, through structured output, `{entities:[{name,type}], relations:[{from,type,to}]}`.

- IDs come from `NormalizeID(name)`. A relation whose endpoint the model did not list creates the entity.
- `Relation.Source` is the document `ID` or `metadata["source"]`, to trace where each triple came from.
- Empty items and duplicates are discarded and counted in `Dropped`. A blank document is skipped without calling the model.
- `Embedder` is optional: without it, the entities have no vector and search falls back to name. If the embedder fails, nothing is written and the error is returned.
- A chunk with invalid output does not bring down the batch: the errors are joined (`errors.Join`, the message cites `chunk N`) and the good chunks are written.
- `Schema.RelationTypes` filters the accepted types. `Schema.EntityTypes` only goes into the prompt; it is not enforced.

**Security.** The relation type comes from an LLM and then becomes a relationship type in Cypher (Neo4j and Memgraph), where it cannot be a parameter.
The extractor normalizes every type to `^[A-Z][A-Z0-9_]*$`: uppercase, any other character becomes `_`, repetitions collapse, leading and trailing `_` are removed,
and an empty type or one starting with a digit is discarded. `treat with; DROP` becomes `TREAT_WITH_DROP`. The normalization is ASCII-only, so accented characters become `_`.

### `Engine`: multi-hop planning

```go
e := &kag.Engine{Model: model, Graph: g, Embedder: emb, Chunks: ragPipeline, Verify: true}
ans, err := e.Answer(ctx, knowledge.Question{Text: "Which patients treated by Dr. Silva had a drug interaction?"})
```

Flow of a question:

1. **Plan.** The model returns, through structured output, a `kag.Plan` with up to `MaxSteps` steps (default 4). Each `PlanStep` has `ID`, `Goal`, `Entity`
   (the starting entity name, or `"$<id>"` to start from the entities reached by an earlier step), `Relations` (types to follow; empty = all),
   `Dir` (`out`, `in` or `both`, default `both`) and `DependsOn`. A reference to an unknown or later step is an error.
2. **Steps.** Each step links the entity to the graph (`FindEntities`, with a vector if there is an `Embedder`) and walks `Neighbors` for up to `MaxHops` hops (default 3).
   The traversal keeps the visited entities, so it terminates on graphs with cycles, and a global set of triples avoids repeated evidence across steps.
3. **Chunk complement.** If a step brings no new triple and `Chunks` is set, the sources of `Chunks.Answer` come in as evidence (only the sources, not the text of the answer).
4. **Synthesis.** The model answers with the numbered evidence (`knowledge.Prompt`). Each triple is a `Source` with `ID` `From|TYPE|To`, content `from —TYPE→ to`
   and `Metadata["source"]` with the origin chunk.
5. **Verification** (`Verify: true`). A second call checks whether the answer is supported by the evidence. If it is not, the answer gets the prefix
   `(not verified)` and the `Trace` gets the `unverified` step with what was missing. A failure of the verification call itself is an error, not a warning.

The `Trace` has a `step:<id>` step for each plan step, with the triples found. `Strategy` is `"kag"`. `Usage` is only that of the synthesis call
(plan and verification do not enter the sum). Each stage emits `callbacks` events, so `tracing` and the LangGraph stream can see the plan and the steps.

#### Limits

- A step whose triples were already collected by an earlier step shows up as `no triples in the graph` and may trigger the chunk complement unnecessarily.
- A step walks up to `MaxHops` hops at once; the planner is not required to take one hop per step.
- The `GraphStore`'s `Path` is not used by the engine today; it stays available for path-explanation tools.
- Plan quality depends on the model. With small models, prefer `Schema.RelationTypes` in the extractor so the vocabulary stays predictable.

## `knowledge.Router`: choosing the technique

```go
r := &knowledge.Router{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine,
    Profile:  knowledge.Profile{CorpusTokens: 40000, ContextWindow: 200000, Volatile: false, HasGraph: true},
    Fallback: true,
}
ans, err := r.Answer(ctx, knowledge.Question{Text: "Who prescribed drug X?"})
```

The router is also an `Answerer`; any of the three techniques may be `nil`. The rules, in order:

1. `Question.Force` (`"rag"`, `"cag"`, `"kag"`) wins. If the forced technique is not configured, the result is `knowledge.ErrNoStrategy`, without falling back to the rules. An invalid value is an error.
2. There is a graph (`Profile.HasGraph`), `KAG` is configured and the `Classifier` says the question is multi-hop: **KAG**. The classifier only runs in this case.
3. Stable corpus (`!Volatile`), `CorpusTokens > 0` and `CorpusTokens <= ContextWindow * 0.8`, with `CAG` configured: **CAG**.
4. Otherwise: **RAG**.

If the chosen technique does not exist, the router uses the first available one in the order RAG, CAG, KAG and says so in the reason. With none: `ErrNoStrategy`.

`Route(ctx, q)` returns only `(strategy, reason, error)`, without answering. In `Answer`, the first step of the `Trace` is `route` (`"<strategy> — <reason>"`),
and `Answer.Strategy` is the technique that actually answered. With `Fallback: true`, an error from the chosen technique (other than RAG) repeats the question on RAG,
and the `Trace` gets the `fallback` step with the original error.

### Multi-hop classifiers

| Type | How it decides |
|---|---|
| `KeywordClassifier` (default) | looks for relation cues, case- and accent-insensitive: `quem`, `entre`, `relaciona*`, `dependência`, `caminho`, `who`, `between`, `related`, `path`, `depends` |
| `LLMClassifier{Model}` | asks the model a yes/no question; an answer outside `sim/não/yes/no` becomes an error |

Implement `knowledge.Classifier` (`MultiHop(ctx, Question) (bool, error)`) for a rule of your own. A classifier failure stops the routing with an error.

### Limit

The `KeywordClassifier` cues are heuristics: "quem" (who) also appears in simple questions. For precision, use `LLMClassifier` or a classifier of your own.

## `knowledge.Toolkit`: tools for agents and MCP server

```go
tk := knowledge.Toolkit{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine, Router: router,
    Ingest: func(ctx context.Context, docs []documents.Document) error { return myIngest(ctx, docs) }, // indexes the documents into your RAG/KAG
}
agent, _ := agents.Create(agents.Options{Model: model, Tools: tk.All()}) // in-process tools
srv := tk.MCPServer(nil)                                                 // the same tools for editors
```

`All()` only includes the tool whose `Answerer` is set:

| Tool | Exists when | Arguments | Does |
|---|---|---|---|
| `rag_answer` | `RAG != nil` | `question`, `force` (optional) | answers by searching the chunks |
| `cag_answer` | `CAG != nil` | same | answers with the corpus in context |
| `kag_query` | `KAG != nil` | same | answers by walking the graph |
| `knowledge_answer` | `Router != nil` | same | the router chooses the technique (description marked `[PRIORITY]`) |
| `knowledge_ingest` | `Ingest != nil` | `documents` (list of `{id?, content, metadata?}`) | calls your `Ingest` |

- The return value is `tools.ContentAndArtifact`: the model sees the text with the numbered sources (`[n] id`), and the `Artifact` keeps the full `knowledge.Answer` (`Trace`, `Usage`).
- The `documents` list arrives as `[]any` (the tool schema rule); in tests, pass `[]any`, not `[]string`.
- A technique error becomes a tool error that the model sees (`tools.Errorf`), without bringing the agent down.
- `MCPServer(onCall)` returns a `server.Server` named `knowledge`, with usage instructions, in the same format as `ciam.Toolkit.MCPServer`. Serve it over stdio (`ServeStdio`) or HTTP (`Handler`).
  `go run ./examples/knowledge/mcp_server -demo` shows a `tools/list` and a `tools/call` in process.

### As a LangGraph node

```go
g.AddNode("respond", knowledge.Node(router, "question", "answer"))
```

`knowledge.Node(a, questionKey, answerKey)` reads `state[questionKey]` (string) and writes `state[answerKey]` with the text and `state[answerKey+"_sources"]` with the sources.

## `integrations/graphstore`: graph on Neo4j and Memgraph

Two adapters of `kag.GraphStore`, both in Cypher. Swap `kag.NewMemory()` for one of them and the rest (extractor, engine, tools) stays the same.

```go
// Neo4j 5.19+ or Aura, through the HTTP Query API
g := &graphstore.Neo4j{URL: "http://localhost:7474", Username: "neo4j", Password: os.Getenv("NEO4J_PASS"), Database: "neo4j", Label: "Entity"}

// Memgraph, through the repository's Bolt client
g := &graphstore.Memgraph{Client: &memgraph.Client{Addr: "localhost:7687"}, Label: "Entity"}
```

`Label` (default `Entity`) separates graphs in the same database: use one label per knowledge base.

### Model in the database

- Entity: node `:<Label>` with `id`, `name`, `type`, `props` (JSON as text), `vector` (list of floats) and `entity = true`. `id` has a uniqueness constraint, created on the first write.
- Relation: directed edge whose **type** is the `Relation.Type`; `props` and `source` live on the edge. A relation to a not-yet-existing entity creates a provisional node (`entity = false`),
  ignored by `Entity()` and `FindEntities()`; when the entity arrives, the node is completed. `kag.Memory` has no provisional nodes.
- `Upsert` uses `MERGE`: repeating a write is idempotent.

### Security

Relation type and label cannot be parameters in Cypher, so they are interpolated into the query text. Therefore:

- a relation type must match `^[A-Z][A-Z0-9_]*$` (the extractor already normalizes it this way); any other value returns an error **before** any request to the database;
- `Label` must match `^[A-Za-z_][A-Za-z0-9_]*$`;
- the `Path` hop limit is a validated, positive integer;
- all values (ids, names, props, vectors) go as parameters, never in the text.

### Differences between the two

| | Neo4j | Memgraph |
|---|---|---|
| Transport | HTTP, `POST /db/<db>/query/v2`, basic auth | Bolt 4.x (`integrations/memgraph`) |
| `Path` | `allShortestPaths((a)-[*1..N]->(b))` | `(a)-[*ALLSHORTEST ..N (r, n \| 1) w]->(b)` |
| Malformed row in `Neighbors` | ignored | error |

On Memgraph, `allShortestPaths(...)` does not exist and `*BFS` returns only one of the minimum paths; `*ALLSHORTEST` returns all of them (verified on a graph with two paths of the same length).

### Limits

- `FindEntities` reads the entities of the label and scores in Go (cosine when both sides have a vector, otherwise the fraction of terms in the name), to give the same result as `kag.Memory`.
  It works well up to a few tens of thousands of entities; beyond that, use the database's vector index (`vector.similarity.cosine` on Neo4j, a vector index on Memgraph) in a future evolution.
- There are no transactions between `UpsertEntities` and `UpsertRelations`: a failure between the two leaves entities without their relations. Re-running the extraction is safe (idempotent).
- Neo4j is spoken only over HTTP (Query API); Bolt for Neo4j is not implemented.

### Tests against the real databases

Contract tests (`httptest`, identifier validation, row conversion) always run. The real tests skip themselves without the environment variable and pass
the `conformance.Graph` suite, the same one used by `kag.Memory`:

```bash
docker run -d --name lcg-neo4j -p 7474:7474 -p 7688:7687 -e NEO4J_AUTH=neo4j/lcgtestpass neo4j:5.26
docker run -d --name lcg-memgraph -p 7687:7687 memgraph/memgraph

NEO4J_URL=http://127.0.0.1:7474 NEO4J_USER=neo4j NEO4J_PASS=lcgtestpass \
MEMGRAPH_ADDR=127.0.0.1:7687 go test -race -count=1 -v ./integrations/graphstore
```

Each run uses a unique label and deletes what it created.

## Which database to combine with each technique

| Technique | Needs | Options in the repository |
|---|---|---|
| RAG | a `vectorstores.VectorStore` | `vectorstores.Memory`; any `Backend` from `integrations/vectorstore` wrapped by `vectorstore.New(embedder, backend)` (`Flat`, `Annoy`, `HNSW`, pgvector, Qdrant, Milvus, Pinecone, Weaviate, Chroma, Elasticsearch, Neo4j, Memgraph and the others on the list in chapter 13) |
| CAG | only the model; the cache belongs to the provider | Anthropic (`cache_control`), OpenAI and compatible (automatic prefix cache) |
| KAG | a `kag.GraphStore` and, optionally, a `VectorStore` for `Chunks` | `kag.Memory`; graph-database adapters in the integrations phase |

RAG is backend-agnostic because it only uses the `VectorStore` interface. The `conformance.RAG(t, newStore)` suite (`internal/conformance/rag.go`) proves it: it indexes
four sources, checks retrieval, metadata filtering, reindexing without changes (`Skipped`), editing one source (`Removed` + `Added`, old text
gone) and the answer with sources. Today it runs over `vectorstores.Memory`, `Flat` and `HNSW`; when adding a backend, call the suite with a constructor for it:

```go
func TestMyBackend(t *testing.T) {
    conformance.RAG(t, func() vectorstores.VectorStore { return vectorstore.New(emb, myBackend()) })
}
```

## `cmd/knowledge`: command line

```bash
go install ./cmd/knowledge

knowledge index docs --glob "*.md" --kind both --out .knowledge   # vectors.json (RAG) and graph.json (KAG)
knowledge ask "What is the warranty of the Zefiro X200?" --dir .knowledge   # the router chooses the technique
knowledge ask "..." --mode cag --src docs                          # CAG: the folder becomes the corpus in context
knowledge mcp --dir .knowledge                                     # MCP server over stdio (editors)
knowledge serve --addr :8080 --dir .knowledge                      # MCP over HTTP
```

| Command | Does |
|---|---|
| `index <path> [--glob] [--kind rag\|kag\|both] [--out]` | loads the files, indexes them (RAG) and/or extracts the graph (KAG) and writes to `--out` (default `./.knowledge`) |
| `ask "question" [--mode auto\|rag\|cag\|kag] [--dir] [--src] [--glob]` | answers; prints the text, the sources (`[n] id  origin`) and the `trace:` |
| `mcp [--dir]` / `serve [--addr] [--dir]` | `knowledge.Toolkit.MCPServer` over stdio or HTTP |

- `--mode auto` uses the `knowledge.Router`: there is a graph when `graph.json` exists; CAG only comes in if `--src` is given. Only in `auto` mode is there a fallback to RAG.
- `kag` without `graph.json` and `cag` without `--src` fail with a message that says what to do.
- Model: `KNOWLEDGE_MODEL` (for example `anthropic:claude-sonnet-5-5`); without it, `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` choose a default; without a key, an offline model that echoes the first context snippet, with a warning. Embeddings: `KNOWLEDGE_EMBEDDINGS` (for example `ollama:nomic-embed-text`); the default is `embeddings.Fake`.
- The offline mode is good for testing the flow. KAG planning requires a real model (`KNOWLEDGE_MODEL`).

### CLI limits

- `index` always rebuilds from scratch; there is no incremental indexing between runs.
- The CLI search is vector-only (K=4): `vectors.json` does not store the list of documents that BM25 needs.
- The CLI's MCP server does not expose `knowledge_ingest`.
- The CLI graph is `kag.Memory` in `graph.json`; for Neo4j or Memgraph, assemble the `kag.Engine` in code.
