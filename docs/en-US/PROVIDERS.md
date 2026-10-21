# LangChain provider coverage

**English** | [Português](../pt-BR/PROVIDERS.md) | [Español](../es-ES/PROVIDERS.md)

Source: <https://docs.langchain.com/oss/python/integrations/providers/all_providers> (505 cards, 504 unique) and the *overview* page.
This table states, without rounding, what exists in this repository. **Implementing all 505 is neither feasible nor useful**: most of them are
third-party community packages (agent identity and payments, guardrails, scraping proxies, memories, caches, crypto)
without a stable public API to port faithfully.

| Status | Count |
|---|---|
| Native (own client) | 9 |
| OpenAI-compatible, default endpoint | 35 |
| OpenAI-compatible, requires `$<PROVIDER>_BASE_URL` | 29 |
| Vector store / database | 26 |
| PostgreSQL + managed pgvector (not tested on that service) | 1 |
| Search / tool / loader | 32 |
| Observability via OTLP/native | 4 |
| **Not implemented** | **368** |

## Implemented

- **Native:** OpenAI, Anthropic, Google, AWS, Cohere, Ollama, Azure AI, IBM, Replicate (`providers/*`; Azure AI = Azure OpenAI).
- **OpenAI-compatible** (`chatmodels.OpenAICompatible`, `provider:model`, key in `$<PROVIDER>_API_KEY`): AI21, AI/ML API, Alibaba Cloud, Baidu, Baseten, Cerebras, DeepSeek, DigitalOcean Gradient AI Platform, Featherless AI, Fireworks, Groq, Helicone, Hugging Face, LiteLLM, LocalAI, MistralAI, ModelScope, Naver, Nebius, NetMind, Nomic, Novita, NVIDIA, OpenRouter, Perplexity, Portkey, PredictionGuard, SambaNova, Sarvam AI, Together, Upstage, VoyageAI, WRITER, XAI, Telnyx.
- **OpenAI-compatible per tenant** (set `$<PROVIDER>_BASE_URL`; without it `Init` returns a clear error): Cloudflare, Databricks, KServe, Ray Serve, Runpod, Snowflake, TrueFoundry, Alephant AI, Apertis, API-Route, Auxen, Brainiall, Cheaper Inference, DaoXE, Doubleword, EmpirioLabs AI, FlexAI, FuturMix, GPTZZZ, GreenNode, Hubris, Interfaze, Neuralwatt, Synthorai, Thalam, TokenMix, Tuning Engines, TrustedRouter, ZeroGPU.
- **Vector stores / databases:** Chroma, Qdrant, Pinecone, Milvus, Weaviate, MongoDB, MongoDB Atlas, Redis, Elasticsearch, AstraDB, Neo4j, SurrealDB, MariaDB, TypesenseVectorStore, FalkorDB, Microsoft, Vectara, ArcadeDB, CrateDB, chDB, CockroachDB, Oracle AI Database, Infinispan, Opensolr, Memgraph, Couchbase (`integrations/vectorstore`, `integrations/redis`, `integrations/mongo`). Checkpointer/store: Redis, MongoDB, DynamoDB, SQLite, PostgreSQL.
- **Pgvector-compatible:** PolarDB PG is PostgreSQL with the pgvector extension; use `vectorstore.PGVector` (not verified on these services).
- **Search, tools and loaders:** Tavily, Exa Search, You.com, LinkUp, SearchApi, SerpApi, Apify, Stack Overflow, Reddit, Hacker News, arXiv, OpenAlexSearch, Firecrawl, Unstructured, ScraperAPI, ScrapingBee, Brightdata, Diffbot, Tigris, XNS, SpiceDB, Jenkins, Kubernetes, AG-UI Protocol, Graph RAG, Zotero, YouTube, Perigon, Oxylabs, Permit, Salesforce, Plivo (`integrations/search`, `integrations/loaders`).
- **Observability:** LangFuse (`tracing/langfuse`); Datadog, MLflow Tracking and OpenInference accept OTLP (`tracing/otlp`, not tested against these destinations).

### Router

`router:<model>` (`providers/router`, via `chatmodels.Init`) sends each call to an ordered list of targets and moves to the next one on a retryable error. Targets come from the environment:

| Variable | Meaning |
|---|---|
| `ROUTER_TARGETS` | Ordered, comma-separated target names (required). `anthropic` and `openai` are native and need `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`; any other name is an OpenAI-compatible gateway and needs `<NAME>_BASE_URL` (optional `<NAME>_API_KEY`). |
| `ROUTER_MODE` | `failover` (default), `round_robin`, `random` or `weighted`: how the first target of each call is picked; the rest follow in list order. |
| `ROUTER_WEIGHTS` | `name:weight,...` for `weighted`. Missing = 1, 0 = never first. |
| `ROUTER_MODEL_<NAME>` | Model for one target; otherwise the model after `router:` is used. |
| `ROUTER_TIMEOUT` | Duration such as `30s`. `Generate`: the whole call. `Stream`: idle timeout (first chunk and each gap between chunks). |

Example: three local gateways plus the two native providers as the last resort.

```bash
export ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
export ROUTER_MODE=failover
export FREELLMAPI_BASE_URL=http://localhost:PORT/v1
export OMNIROUTE_BASE_URL=http://localhost:PORT/v1
export BIFROST_BASE_URL=http://localhost:PORT/v1
export FREELLMAPI_API_KEY=...
export OMNIROUTE_API_KEY=...
export BIFROST_API_KEY=...
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
export ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
export ROUTER_TIMEOUT=60s
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // same model on every target unless ROUTER_MODEL_<NAME> overrides it
resp, err := m.Generate(ctx, msgs)
```

- **Retryable** (next target): network errors, timeouts, HTTP 408, 425, 429 and 5xx. **Final** (returned at once): 400, 401, 403, 404 and other 4xx, and cancellation of the caller's `ctx`.
- Every target receives the same full messages and options, so context is kept on failover.
- Streaming: a failure before any text moves to the next target with the original messages. A failure after text (and no tool call) sends the next target the original messages, the partial assistant text and a "continue" user message; the caller sees one continuous answer (quality depends on the model). A partial tool call cannot be continued: the error is returned.
- The `round_robin` counter belongs to one router instance (one `Init` call), so one process per agent rotates independently.

The Helicone and Portkey gateways work as OpenAI-compatible providers (proxy). See also [INTEGRATIONS.md](INTEGRATIONS.md).

## Not implemented (368)

Extension paths for any of them: a `tools.Tool` (`tools.New`), an MCP server (`mcp.LoadTools`), a `vectorstore.Backend`
(3 methods), a `checkpoint.Saver`/`store.Store`, a `callbacks.Handler`, or a file/execution `Backend` in `deepagents/backends`.
Among them are real and relevant integrations left out for lack of a verifiable API here, or because they require a native driver/SDK:
IBM (watsonx), OCI, Microsoft, Replicate, Apple Foundation Models (models); Couchbase, Teradata, Kinetica, SingleStore, OceanBase,
Oracle AI Database, SAP, YDB, Aerospike, Vectara, Vectorize, DeepLake, Memgraph, Kuzu, ScyllaDB (databases); E2B, Daytona, Modal,
Runloop (sandboxes); Browserbase, Oxylabs, Docling, MinerU, Airbyte, Box (collection and loaders).

1Claw, AACP, Abso, Actian VectorAI DB, Adeu, Ads4GPTs, Aerospike, Agent Action Capsule, Agent Evidence, Agent Traffic Lab, AgentGate, Agentic SpendGuard, AgenticEmail, AgentLair, AgentLine, AgentMail, AgentOps Cockpit, AgentPhone, AgentQL, AgentRAM, AgentSeal, agentstate, AgentSystems, AI Identity, Aidress, Airbyte, Alchemyst AI, AlexEvidenceVerifier, Algenta, AlphaAI, Ampersend, AMUVectorStore, Anakin, Anchor Browser, ANEForge, AntiBrow, any-llm, AnyAPI, Apple Foundation Models, AproxPay, Arcmira, ARO, Asqav, assistant-ui, Asyntai, Atomic Mail, ATR Guardrail, Attenu Guard, Ausca, Axiora, AxioRank, Baponi, Bastion Prompt Protection, Bidda, Blindfold, Blopus, Bodo DataFrames, BowmarkToolkit, Box, Browserbase, CAMB AI, Capslane, Capsule, Ceki, ChronoVec, Clearsigned, cloro, Coalent, Cognee, comply54, Confident AI, Context, Context.dev, Contextual, CopilotKit, Cosmergon, CosVectors, Crawleo, CrustAPI, CRW, CTRLRun, CVFile, Dappier, Daytona, DeepKeep, DeepLake, Delegare, Dell, Dewey, DexPaprika, Discord Shikenso, Distil, DNS Doctor, Docling, Docugami, DomPruner, DProvenanceKit, Duel Agents, DynamicToolSelectorMiddleware, E2B, Egnyte, EnConvert, Engram, ERC-8004, ERC20, FeedMyAgent, FiGuard, Firebolt, Fizzl, FlashDataToolkit, FlatmarkLoader, FlipCoin, FMP Data, Foliant, Football Charts, FoundryNet, FoxNose, GAIP, Galaxia, Gandr, GateCore, Gel, GetYouTubeTranscript, GoAT, GoodMem, GoodSender, Graceful Fail, GraphSignal, GreenCalculus, GridGain, Haldir, Hashlock, Highflame, HighSNR, Hindsight, HippoDid, Hlido, HuangtingFlux, HWP/HWPX, Hydrafetch, HyperBrowser, ICTContact, ICTExam, ICTFax, ICTPBX, iGPT, iLoveVideoEditor, Inferrail, Infino, Inspeximus, Instanode, Interven, Isaacus, Islo, ISNAD, John Snow Labs, Kalibr, Kavel, Keenable, Keiro, Kerq, Kinetica, KoNLPy, Kuzu, Kyvvu, LambdaDB, LangChain Decorators, LangChain Replay, langchain-arabic, LangFair, LatAm Synth, Leap0, LexMex, Lians, Lindorm, Lithtrix, Live Tennis API, Lizard, Log10, LongProbe, Lyrenth, m3-memory, Machine Library, MadeOnSol, Magic Hour, MartToolkit, MaskFlow, MemoryRouter, Memstate AI, Mengram, MeragptDecider, Metal, MindsDB, MinerU, MinnsDB, Mixpeek, mnki (Agent Trust), Modal, Modexia, Moorcheh, Motorhead, MrScraper, Ned Watch, NeuralTrust, NewsCatcher, Nexus Shield, Nia, Nimble, NodeProxy, nono, Notte, Noveum, Nuggets, OceanBase, OCI, Octen, OMEM, Opedd, Open Agent Spec, OpenBox, OpenDataLoader PDF, OpenDMA, OpenGradient, OpenMemory, OpenSandbox, OpenTTT, OpenZoo, Opsen, OrcaCallbackHandler, oxidize-pdf, Parallel, PaySafe, pdfmuse, Perseus Vault, Pexafy, PIIGhost, Pinchwork, Pipeshift, PlainId, Plasmate, Play Store, Polaris, PolarisAIDataInsight, Polign, POP, PQS, pr402, Pramagent, Privy, Prolog, prompt-shield, PromptfirewallGuard, ProxyClaw, ProxyHat, Pushary, PyMuPDF4LLM, QuanticData, Querit, RE-call, RelayShield, Remembrall, ReplyLayer, Reserp, Respan, Robocorp, Rostam, Runcycles, Runloop, Runtime, RustChain, SafePrompt, Sail, SAP, Scavio, SchemagateRetriever, ScoutWyze Compute, ScrapeGraph, Scrapeless, ScyllaDB, seekrit, Semantica, SereneDB, SERPdive, Serpex, SerpKite, Shale Protocol, ShannonBase, SibFly, SidClaw, Signatrust, Signet, Simplepages, SingleStore, SkillLite, Skim, SnapRender, Soniox, Soul, Sourcey, Spanlens, Spidra, Stardog, Strale, Sulci Cache, Supadata, SuperColony, SuperLocalMemory, Superserve, SupraWall, Synap, Synmerco, Tableau, Taiga, TalorData, TanodToolkit, TealTiger, TempGuru, Tendem, TensorLake, Tenuo, Teradata, Tessera, The Context Company, ThorData, Tilores, Timbr, Tokonomics, TokPortal, Tonic Textual, Toolbox LangChain, Toolstem, Traccia, TrueLens, Truth Bear, TypeDB, TypeSafe, UnDatasIO, UniRate, Uniswap, UploadPostToolkit, Upstash Box, URLpipe, Valthera, Valyu, VAST Data, Vaultak, VDMS, vecr-compress, Vector Panda, Vectorize, Vequil, Vercel, Verifly, Veritensor, Voxell, WaveSpeed, Web Metadata Extractor, Webz, Whoosh, Work Ledger, x402, xaidr, Xpoz, YantrikDB, YDB, YeagerAI, Yutori, ZeusDB
