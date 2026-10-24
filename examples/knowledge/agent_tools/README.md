# agent_tools

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

An agent that answers from the knowledge base by calling `knowledge.Toolkit` tools (`rag_answer`, ...). The agents run through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation has a real share of code, JSON tool results become TOON, all decided by rules with no model needed.

## What it does

- Two chunks (a prescription and a drug note) are indexed in an in-memory vector store behind a `rag.Pipeline`.
- `knowledge.Toolkit` exposes the pipeline and a router as tools (`kb.All()`); the scripted agent calls `rag_answer`, receives the cited answer and replies with `[1]`.

## How to apply it

```go
kb := knowledge.Toolkit{RAG: rg, Router: &knowledge.Router{RAG: rg}}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kb.All(), SystemPrompt: "Answer from the knowledge base and cite sources."})
```

Give an agent knowledge as tools when it should decide **when** to look things up. Add `CAG` and `KAG` to the toolkit and the router picks the technique per question.

## Run it

```bash
go run ./examples/knowledge/agent_tools
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use a real embedder and a durable store, and a real model for the pipeline and for the agent.
2. Index your real corpus with `cmd/knowledge index` and check the answers cite the right sources.
3. Test questions the corpus cannot answer: the agent should say so instead of guessing.
4. Keep source metadata (file, section) on every chunk; citations are only as good as the metadata.

## See also

- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
