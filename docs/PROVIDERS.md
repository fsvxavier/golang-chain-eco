# Cobertura dos provedores da LangChain

Fonte: <https://docs.langchain.com/oss/python/integrations/providers/all_providers> (505 cartões, 504 únicos) e a página *overview*.
Esta tabela diz, sem arredondar, o que existe neste repositório. **Implementar "todos" os 505 não é viável nem útil**: a maioria é
um pacote comunitário de terceiros (identidade e pagamento de agentes, guardrails, proxies de scraping, memórias, caches, cripto)
sem API pública estável para portar com fidelidade.

| Situação | Qtde |
|---|---|
| Nativo (cliente próprio) | 7 |
| OpenAI-compatível, endpoint padrão | 34 |
| OpenAI-compatível, exige `$<PROVEDOR>_BASE_URL` | 7 |
| Vector store / banco | 15 |
| PostgreSQL + pgvector gerenciado (não testado nesse serviço) | 1 |
| Busca / ferramenta / loader | 18 |
| Observabilidade via OTLP/nativo | 4 |
| **Não implementados** | **418** |

## Implementados

- **Nativos:** OpenAI, Anthropic, Google, AWS, Cohere, Ollama, Azure AI (`providers/*`; Azure AI = Azure OpenAI).
- **OpenAI-compatíveis** (`chatmodels.OpenAICompatible`, `provedor:modelo`, chave em `$<PROVEDOR>_API_KEY`): AI21, AI/ML API, Alibaba Cloud, Baidu, Baseten, Cerebras, DeepSeek, DigitalOcean Gradient AI Platform, Featherless AI, Fireworks, Groq, Helicone, Hugging Face, LiteLLM, LocalAI, MistralAI, ModelScope, Naver, Nebius, NetMind, Nomic, Novita, NVIDIA, OpenRouter, Perplexity, Portkey, PredictionGuard, SambaNova, Sarvam AI, Together, Upstage, VoyageAI, WRITER, XAI.
- **OpenAI-compatíveis por tenant** (defina `$<PROVEDOR>_BASE_URL`; sem ela `Init` devolve erro claro): Cloudflare, Databricks, KServe, Ray Serve, Runpod, Snowflake, TrueFoundry.
- **Vector stores / bancos:** Chroma, Qdrant, Pinecone, Milvus, Weaviate, MongoDB, MongoDB Atlas, Redis, Elasticsearch, AstraDB, Neo4j, SurrealDB, MariaDB, TypesenseVectorStore, FalkorDB (`integrations/vectorstore`, `integrations/redis`, `integrations/mongo`). Checkpointer/store: Redis, MongoDB, DynamoDB, SQLite, PostgreSQL.
- **Pgvector-compatíveis:** PolarDB PG é PostgreSQL com a extensão pgvector; use `vectorstore.PGVector` (não verificado nesses serviços).
- **Busca, ferramentas e loaders:** Tavily, Exa Search, You.com, LinkUp, SearchApi, SerpApi, Apify, Stack Overflow, Reddit, Hacker News, arXiv, OpenAlexSearch, Firecrawl, Unstructured, ScraperAPI, ScrapingBee, Brightdata, Diffbot (`integrations/search`, `integrations/loaders`).
- **Observabilidade:** LangFuse (`tracing/langfuse`); Datadog, MLflow Tracking e OpenInference aceitam OTLP (`tracing/otlp`, não testado nesses destinos).

Os gateways Helicone e Portkey funcionam como provedores OpenAI-compatíveis (proxy). Ver também [INTEGRATIONS.md](INTEGRATIONS.md).

## Não implementados (418)

Caminhos de extensão para qualquer um deles: uma `tools.Tool` (`tools.New`), um servidor MCP (`mcp.LoadTools`), um `vectorstore.Backend`
(3 métodos), um `checkpoint.Saver`/`store.Store`, um `callbacks.Handler` ou um `Backend` de arquivos/execução em `deepagents/backends`.
Entre eles há integrações reais e relevantes que ficaram de fora por falta de API verificável aqui ou por exigirem driver/SDK nativo:
IBM (watsonx), OCI, Microsoft, Replicate, Apple Foundation Models (modelos); Couchbase, Teradata, Kinetica, SingleStore, OceanBase,
Oracle AI Database, SAP, YDB, Aerospike, Vectara, Vectorize, DeepLake, Memgraph, Kuzu, ScyllaDB (bancos); E2B, Daytona, Modal,
Runloop (sandboxes); Browserbase, Oxylabs, Docling, MinerU, Airbyte, Box (coleta e loaders).

1Claw, AACP, Abso, Actian VectorAI DB, Adeu, Ads4GPTs, Aerospike, AG-UI Protocol, Agent Action Capsule, Agent Evidence, Agent Traffic Lab, AgentGate, Agentic SpendGuard, AgenticEmail, AgentLair, AgentLine, AgentMail, AgentOps Cockpit, AgentPhone, AgentQL, AgentRAM, AgentSeal, agentstate, AgentSystems, AI Identity, Aidress, Airbyte, Alchemyst AI, Alephant AI, AlexEvidenceVerifier, Algenta, AlphaAI, Ampersend, AMUVectorStore, Anakin, Anchor Browser, ANEForge, AntiBrow, any-llm, AnyAPI, Apertis, API-Route, Apple Foundation Models, AproxPay, ArcadeDB, Arcmira, ARO, Asqav, assistant-ui, Asyntai, Atomic Mail, ATR Guardrail, Attenu Guard, Ausca, Auxen, Axiora, AxioRank, Baponi, Bastion Prompt Protection, Bidda, Blindfold, Blopus, Bodo DataFrames, BowmarkToolkit, Box, Brainiall, Browserbase, CAMB AI, Capslane, Capsule, Ceki, chDB, Cheaper Inference, ChronoVec, Clearsigned, cloro, Coalent, CockroachDB, Cognee, comply54, Confident AI, Context, Context.dev, Contextual, CopilotKit, Cosmergon, CosVectors, Couchbase, CrateDB, Crawleo, CrustAPI, CRW, CTRLRun, CVFile, DaoXE, Dappier, Daytona, DeepKeep, DeepLake, Delegare, Dell, Dewey, DexPaprika, Discord Shikenso, Distil, DNS Doctor, Docling, Docugami, DomPruner, Doubleword, DProvenanceKit, Duel Agents, DynamicToolSelectorMiddleware, E2B, Egnyte, EmpirioLabs AI, EnConvert, Engram, ERC-8004, ERC20, FeedMyAgent, FiGuard, Firebolt, Fizzl, FlashDataToolkit, FlatmarkLoader, FlexAI, FlipCoin, FMP Data, Foliant, Football Charts, FoundryNet, FoxNose, FuturMix, GAIP, Galaxia, Gandr, GateCore, Gel, GetYouTubeTranscript, GoAT, GoodMem, GoodSender, GPTZZZ, Graceful Fail, Graph RAG, GraphSignal, GreenCalculus, GreenNode, GridGain, Haldir, Hashlock, Highflame, HighSNR, Hindsight, HippoDid, Hlido, HuangtingFlux, Hubris, HWP/HWPX, Hydrafetch, HyperBrowser, IBM, ICTContact, ICTExam, ICTFax, ICTPBX, iGPT, iLoveVideoEditor, Inferrail, Infinispan, Infino, Inspeximus, Instanode, Interfaze, Interven, Isaacus, Islo, ISNAD, Jenkins, John Snow Labs, Kalibr, Kavel, Keenable, Keiro, Kerq, Kinetica, KoNLPy, Kubernetes, Kuzu, Kyvvu, LambdaDB, LangChain Decorators, LangChain Replay, langchain-arabic, LangFair, LatAm Synth, Leap0, LexMex, Lians, Lindorm, Lithtrix, Live Tennis API, Lizard, Log10, LongProbe, Lyrenth, m3-memory, Machine Library, MadeOnSol, Magic Hour, MartToolkit, MaskFlow, Memgraph, MemoryRouter, Memstate AI, Mengram, MeragptDecider, Metal, Microsoft, MindsDB, MinerU, MinnsDB, Mixpeek, mnki (Agent Trust), Modal, Modexia, Moorcheh, Motorhead, MrScraper, Ned Watch, NeuralTrust, Neuralwatt, NewsCatcher, Nexus Shield, Nia, Nimble, NodeProxy, nono, Notte, Noveum, Nuggets, OceanBase, OCI, Octen, OMEM, Opedd, Open Agent Spec, OpenBox, OpenDataLoader PDF, OpenDMA, OpenGradient, OpenMemory, OpenSandbox, Opensolr, OpenTTT, OpenZoo, Opsen, Oracle AI Database, OrcaCallbackHandler, oxidize-pdf, Oxylabs, Parallel, PaySafe, pdfmuse, Perigon, Permit, Perseus Vault, Pexafy, PIIGhost, Pinchwork, Pipeshift, PlainId, Plasmate, Play Store, Plivo, Polaris, PolarisAIDataInsight, Polign, POP, PQS, pr402, Pramagent, Privy, Prolog, prompt-shield, PromptfirewallGuard, ProxyClaw, ProxyHat, Pushary, PyMuPDF4LLM, QuanticData, Querit, RE-call, RelayShield, Remembrall, Replicate, ReplyLayer, Reserp, Respan, Robocorp, Rostam, Runcycles, Runloop, Runtime, RustChain, SafePrompt, Sail, Salesforce, SAP, Scavio, SchemagateRetriever, ScoutWyze Compute, ScrapeGraph, Scrapeless, ScyllaDB, seekrit, Semantica, SereneDB, SERPdive, Serpex, SerpKite, Shale Protocol, ShannonBase, SibFly, SidClaw, Signatrust, Signet, Simplepages, SingleStore, SkillLite, Skim, SnapRender, Soniox, Soul, Sourcey, Spanlens, SpiceDB, Spidra, Stardog, Strale, Sulci Cache, Supadata, SuperColony, SuperLocalMemory, Superserve, SupraWall, Synap, Synmerco, Synthorai, Tableau, Taiga, TalorData, TanodToolkit, TealTiger, Telnyx, TempGuru, Tendem, TensorLake, Tenuo, Teradata, Tessera, Thalam, The Context Company, ThorData, Tigris, Tilores, Timbr, TokenMix, Tokonomics, TokPortal, Tonic Textual, Toolbox LangChain, Toolstem, Traccia, TrueLens, TrustedRouter, Truth Bear, Tuning Engines, TypeDB, TypeSafe, UnDatasIO, UniRate, Uniswap, UploadPostToolkit, Upstash Box, URLpipe, Valthera, Valyu, VAST Data, Vaultak, VDMS, vecr-compress, Vectara, Vector Panda, Vectorize, Vequil, Vercel, Verifly, Veritensor, Voxell, WaveSpeed, Web Metadata Extractor, Webz, Whoosh, Work Ledger, x402, xaidr, XNS, Xpoz, YantrikDB, YDB, YeagerAI, YouTube, Yutori, ZeroGPU, ZeusDB, Zotero
