# 20. RAG, CAG and KAG

**English** | [Português](../../pt-BR/manual/20-rag-cag-kag.md) | [Español](../../es-ES/manual/20-rag-cag-kag.md)

Three ways to give knowledge to a model, behind the same interface: `knowledge.Answerer` receives a question and returns text, sources, the strategy used and a trace (`Trace`) of the decisions.
This chapter shows when to use each one, how to assemble it, how to let the router choose, and how to connect everything to agents and MCP.
Type reference: [KNOWLEDGE.md](../KNOWLEDGE.md).

## 20.1 Which one to use

| | RAG | CAG | KAG |
|---|---|---|---|
| Idea | fetch the closest passages and inject them into the prompt | put the whole corpus in the prompt and let the provider cache it | extract a graph of entities and relations and reason across hops |
| Package | `rag` | `cag` | `kag` |
| Data scale | high (millions of passages) | low (fits in the model window) | medium to high (depends on the modeling) |
| Latency per question | medium | low (cached prefix) | higher (plan, hops, synthesis) |
| Multi-hop question | weak if the answer is spread over separate passages | good, the model sees everything | best case |
| Updating data | easy (reindex the source) | swap the corpus and lose the cache | re-extract the changed passages |
| Cost per question | medium | low with cache, high without | more model calls |

Rule of thumb: thousands of documents or a base that changes constantly: **RAG**. Few stable documents and minimal latency: **CAG**. Questions like "who is related to whom" or "what is the root cause": **KAG**. When in doubt, `knowledge.Router` decides for you (section 20.5).

## 20.2 RAG

```go
emb := embeddings.Fake{Dim: 256} // swap for a real embedder
store := vectorstores.NewMemory(emb) // or any VectorStore from integrations/vectorstore

idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
if _, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil)); err != nil { /* ... */ }

p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, // vector + BM25 by RRF
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "How do I reset the Router-X500 password?"})
fmt.Println(ans.Text)
for _, s := range ans.Sources { fmt.Println(s.Metadata["source"]) }
```

- The answer cites sources as `[1]`, `[2]`, in the order of `ans.Sources`.
- `Hybrid` finds exact terms (error codes, product names) that vector search misses. See `go run ./examples/knowledge/rag`.
- Reindexing is cheap: a source with the same content is skipped (`Skipped`); a changed one replaces only its own chunks.
- Want to reorder with a rerank model? Fill `Reranker` with the result of `chatmodels.NewReranker("cohere:<model>")` (also `jina:` and `voyageai:`).
- Filter by metadata: `knowledge.Question{Filter: vectorstores.MetadataEquals(map[string]any{"product": "x500"})}`.

Combine it with any vector backend from chapter 13 (pgvector, Qdrant, Milvus, Chroma, Elasticsearch, Neo4j, Memgraph...): the `Pipeline` only sees a `vectorstores.VectorStore`.

## 20.3 CAG

```go
corpus := cag.NewCorpus(docs) // input order does not matter
e := &cag.Engine{
    Model:    chatmodels.MustInit("anthropic:claude-sonnet-5-5"),
    Corpus:   corpus,
    Window:   200000,
    Fallback: p, // the RAG pipeline above, used if the corpus does not fit
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "What is the termination fee with Supplier Y?"})
```

- The corpus goes in the `system` message and the history after it, so the prefix stays identical across questions and the provider's cache hits.
- Anthropic: the block gets `cache_control: ephemeral`. OpenAI and compatible: automatic prefix caching. Other providers work, without savings.
- To confirm the cache worked, look at `ans.Usage.CacheReadTokens` or the `cache_hit` step in the `Trace`.
- Changed corpus: `e.SetCorpus(new)` returns `true` and the provider's cache will be rebuilt on the next call.
- Corpus above `Window - Reserve`: uses the `Fallback`; without a `Fallback`, `cag.ErrCorpusTooLarge`.

The token count is an estimate (~4 characters per token). Near the limit, inject your own `knowledge.TokenCounter` into `Engine.Counter`.

## 20.4 KAG

```go
g := kag.NewMemory() // or &graphstore.Neo4j{...} / &graphstore.Memgraph{...} for a graph database

x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCRIBED", "INTERACTS_WITH", "PRESCRIBED_TO"}}}
stats, err := x.Extract(ctx, chunks) // chunks: []documents.Document

e := &kag.Engine{Model: model, Graph: g, Embedder: emb, Chunks: p, Verify: true}
ans, err := e.Answer(ctx, knowledge.Question{
    Text: "Who prescribed a drug that interacts with the Aspirin prescribed to John?"})
for _, st := range ans.Trace { fmt.Println(st.Name, st.Detail) } // one line per hop
```

- The extractor turns each passage into entities and relations; each triple keeps the source passage. Empty items and duplicates are discarded.
- The engine plans the question into steps, walks the graph (without repeating triples, even with cycles) and synthesizes with the numbered evidence. The sources of the answer are the triples used.
- `Verify: true` checks the answer against the evidence and marks `(not verified)` when something is not supported.
- `Chunks` (a RAG `Answerer`) completes steps where the graph has nothing.
- Use `Schema.RelationTypes` to keep the vocabulary of relations stable; the type is normalized to `UPPERCASE_WITH_UNDERSCORE` before it reaches the graph.

`go run ./examples/knowledge/kag` shows vector search alone failing the 3-hop question and the engine answering it.

## 20.5 Router

```go
r := &knowledge.Router{
    RAG: p, CAG: e1, KAG: e2,
    Profile:  knowledge.Profile{CorpusTokens: 40000, ContextWindow: 200000, Volatile: false, HasGraph: true},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: "Who is related to contract Y?"})
fmt.Println(ans.Strategy, ans.Trace[0]) // kag, route: kag — multi-hop question with a graph available
```

The first step of the `Trace` is always the decision (`route`). You can force the technique per question with `Question.Force`. Order of the rules: Force, then multi-hop with a graph (KAG), then a stable corpus that fits in 80% of the window (CAG), otherwise RAG. With `Fallback: true`, an error in CAG or KAG repeats the question on RAG, and the `Trace` records it.
The default multi-hop classifier is a word heuristic (pt-BR and en); use `knowledge.LLMClassifier{Model: m}` when you need more precision.

## 20.6 Integrations

**Tools for agents and MCP.** A `knowledge.Toolkit` becomes tools (`rag_answer`, `cag_answer`, `kag_query`, `knowledge_answer`, `knowledge_ingest`) for `agents.Create`
and an MCP server for editors, with the same tools:

```go
tk := knowledge.Toolkit{RAG: p, CAG: e1, KAG: e2, Router: r}
agent, _ := agents.Create(agents.Options{Model: model, Tools: tk.All()})
srv := tk.MCPServer(nil) // srv.ServeStdio(ctx, os.Stdin, os.Stdout)
```

`go run ./examples/knowledge/agent_tools` shows the agent calling `rag_answer`; `go run ./examples/knowledge/mcp_server -demo` shows `tools/list` and `tools/call` in process.
Inside a LangGraph graph, use `knowledge.Node(r, "question", "answer")`.

**Graph databases.** `graphstore.Neo4j` and `graphstore.Memgraph` implement `kag.GraphStore`; swapping `kag.NewMemory()` for them changes nothing else. Relation types are validated
(`^[A-Z][A-Z0-9_]*$`) before they enter the Cypher query. Details, differences and how to test against Docker in [KNOWLEDGE.md](../KNOWLEDGE.md).

**Vector databases.** RAG accepts any `vectorstores.VectorStore`; `vectorstore.New(embedder, backend)` wraps the backends of chapter 13. The `conformance.RAG` suite guarantees the same behavior
on `Memory`, `Flat` and `HNSW`.

**Command line.**

```bash
go run ./cmd/knowledge index docs --kind both
go run ./cmd/knowledge ask "Who is related to contract Y?"
```

## 20.7 Recipes

- **Each technique behind a single interface.** Application code receives a `knowledge.Answerer`; swapping RAG for CAG changes nothing else.
- **As a chain/Runnable:** `knowledge.AsRunnable(a)` returns a `runnables.Runnable[Question, Answer]`.
- **Measuring cache savings:** sum `Usage.CacheReadTokens` from the answers; `InputTokens` still sums everything.
- **Persisting:** `vectorstores.Memory.Save/Load` and `kag.Memory.Save/LoadMemory` write JSON; in production use a real vector backend (chapter 13).

## 20.8 Known limits

- RAG's incremental indexing keeps the hashes only in the process memory; restarting reindexes everything. Sources that disappear from the loader are not removed from the store.
- The pipeline's BM25 is rebuilt on every question (fine up to a few thousand passages).
- CAG depends on the provider's cache, which expires and charges for the first write; the token count is estimated.
- In KAG, the quality of the plan and of the extraction comes from the model. A step whose triples were already collected by another step may trigger the chunk complement unnecessarily.
- KAG's `Usage` covers only the synthesis call (plan, extraction and verification do not enter the sum).
- `FindEntities` in the Neo4j and Memgraph adapters scores in Go after reading the entities of the label; for very large graphs, the database's vector index is still missing.

## 20.9 Common problems

| Symptom | Likely cause |
|---|---|
| `cag: corpus does not fit the context window` | corpus larger than `Window - Reserve`; set `Fallback` or reduce the corpus |
| `Cache read` always zero on Anthropic | prefix changed between calls (corpus changed, different instruction) or the cache expired |
| KAG answers "I don't know" | empty graph or extraction with no triples (see `ExtractStats.Dropped`); set `Chunks` as a complement |
| `rag: cannot answer with strategy "kag"` | `Question.Force` points to another technique; let the router decide or clear the field |
| `kag needs graph.json` / `cag needs --src` (CLI) | the requested mode needs `index --kind kag` or `--src` |
| `graphstore: relation type ... must match` | relation type outside `^[A-Z][A-Z0-9_]*$`; use `kag.Extractor`, which normalizes it |
| `knowledge: no strategy available` | `Router` with no technique configured, or `Force` to a `nil` technique |

---

Previous: [19. Open SWE](19-open-swe.md) · [Manual index](README.md)
