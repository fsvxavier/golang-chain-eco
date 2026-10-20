# Analysis of the remaining 368 providers

**English** | [Português](../pt-BR/PROVIDERS-ANALISE.md) | [Español](../es-ES/PROVIDERS-ANALISE.md)

Source of the names and descriptions: cards at <https://docs.langchain.com/oss/python/integrations/providers/all_providers>.
Criterion for "not third-party": a product of an established company or a known open-source project, with a public, documented API/protocol,
and not merely a community add-on for agents (governance, identity, x402 payments, crypto, startup memories).
Criterion for "can be implemented": it is possible to write the client in Go with the standard library and **prove** that it works, either against the real service
(Docker, emulator) or, at minimum, against a test server that reproduces the public contract.

| Group | Count | What it means |
|---|---|---|
| **A. Implementable and truly testable** | 0 | open-source service/emulator in Docker, or an offline adapter |
| **B. Product/vendor with public API** | 123 | implementable, but only with an `httptest` test, or requires a license/account/driver |
| **C. OpenAI-compatible gateways** | 0 | 1 line in the `OpenAICompatible` table, with `$<PROVIDER>_BASE_URL` (official URL not verified) |
| **D. Tied to a language/platform** | 24 | Python/Rust/Swift library or frontend; nothing to port |
| **E. Community/niche** | 221 | discarded |

## A. Implementable and testable (0 pending; 16 already implemented: ArcadeDB, CrateDB, chDB, CockroachDB, Oracle AI Database, Infinispan, Opensolr, Memgraph, Couchbase, Tigris, XNS, SpiceDB, Jenkins, Kubernetes, AG-UI Protocol, Graph RAG)

| Provider | Effort | How to test | Note |
|---|---|---|---|

Effort: S = hours, M = half a day, L = a day or more. Three of them unlock several cards at once: the **S3 client** (Tigris, XNS and AWS S3 itself, Lambda and SQS via LocalStack), **ClickHouse** (covers chDB) and the **SQL test driver** (Oracle, CockroachDB).

## B. Products with public API, no real test possible here (123 pending; 12 implemented with contract tests: Replicate, IBM, Microsoft, Salesforce, Vectara, Zotero, YouTube, Plivo, Telnyx, Permit, Oxylabs, Perigon)

Implementable, but I would only be able to validate against a test server written by me from the contract; the risk is that the real API diverges.
Suggested priority (mature, documented APIs): IBM watsonx, Microsoft (Azure AI Search), Replicate, Vectara, Zotero, YouTube Data API, Plivo/Telnyx,
Permit, Oxylabs, Parallel, Perigon and the Daytona, E2B and Runloop sandboxes.

| Provider | Contract / limitation |
|---|---|
| Actian VectorAI DB | embedded/cloud vector database |
| Aerospike | own binary protocol |
| AgentQL | natural-language query |
| AgentRAM | REST |
| Airbyte | connections/jobs API |
| Alchemyst AI | REST |
| Anakin | REST scraping |
| Anchor Browser | sessions + CDP |
| Baponi | REST |
| Box | REST + OAuth (loader) |
| Browserbase | sessions + CDP (needs a CDP client) |
| CAMB AI | TTS/dubbing REST |
| Capsule | WebAssembly (wazero in Go would be the path) |
| Cognee | open source, REST /api/v1 |
| Confident AI | OTLP/REST (DeepEval) |
| Context | REST |
| Context.dev | scraping/extraction REST |
| Contextual | rerank/generation (api.contextual.ai) |
| CosVectors | Tencent COS Vectors, signed API |
| Crawleo | search/crawl REST |
| CRW | Firecrawl-compatible: just point at the Firecrawl base URL |
| Dappier | AI data API |
| Daytona | REST + toolbox API |
| DeepLake | Activeloop; own format, Python SDK |
| Dell | Dell AI Factory/Enterprise Hub (API to be confirmed) |
| Dewey | document/retriever REST API |
| Docling | docling-serve REST (/v1alpha/convert); the library is Python |
| Docugami | document REST API |
| E2B | REST + Connect-RPC (envd) |
| Egnyte | REST + OAuth (loader/search) |
| Engram | REST |
| Firebolt | HTTP API + vector_search; cloud only |
| Gandr | TTS REST |
| GetYouTubeTranscript | transcripts REST |
| GoodMem | REST |
| GraphSignal | SDK/API |
| GridGain | Ignite binary thin client (checkpointer/store/cache) |
| Hindsight | open source, REST |
| HyperBrowser | scrape/sessions REST |
| Infino | REST API |
| Isaacus | legal models (embedding/classification) via API |
| Islo | REST |
| Kavel | image generation REST |
| Keenable | search API |
| Keiro | search API |
| Kinetica | REST/SQL with vectors; image requires a license |
| LambdaDB | serverless REST API |
| Leap0 | REST |
| Lindorm | Alibaba Cloud multi-model (compatible SQL/ES) |
| Lizard | REST |
| Log10 | REST API |
| Magic Hour | media generation REST |
| Mengram | REST |
| Metal | discontinued |
| MindsDB | open source; SQL/HTTP, docker image available |
| MinerU | service REST API; the library is Python |
| Mixpeek | multimodal REST API; cloud only |
| Modal | gRPC SDK |
| Moorcheh | REST API; cloud only |
| MrScraper | REST scraping |
| NewsCatcher | news REST |
| Nia | REST |
| Nimble | web data REST |
| Notte | sessions + CDP |
| Noveum | traces REST |
| OceanBase | MySQL protocol + cosine_distance; heavy image |
| OCI | Generative AI with signed requests (RSA) |
| Octen | search API |
| OpenSandbox | open-source server (API to be confirmed) |
| Opsen | REST |
| Parallel | Search/Extract API |
| PlainId | PDP REST |
| Play Store | unofficial scraping |
| Polign | REST API |
| QuanticData | SERP/crawl REST |
| Querit | search API |
| Respan | gateway + traces |
| Rostam | own binary (vector + KV) |
| Runloop | REST (devboxes) |
| Runtime | Firecracker microVMs, REST |
| SAP | HANA Cloud (go-hdb driver) and Generative AI Hub |
| Scavio | search API |
| ScrapeGraph | smartscraper REST |
| Scrapeless | scraping REST |
| ScyllaDB | binary CQL; vector search only in recent/Cloud versions |
| SereneDB | PostgreSQL-compatible (pgvector likely; no verified image) |
| SERPdive | search API |
| Serpex | search API |
| SerpKite | search API |
| SingleStore | MySQL protocol + DOT_PRODUCT; image requires a license |
| SnapRender | screenshots REST |
| Soniox | STT REST/WebSocket |
| Spanlens | callback + REST |
| Spidra | scraping REST |
| Stardog | SPARQL HTTP; commercial license |
| Supadata | transcripts REST |
| Superserve | REST |
| Synap | REST |
| Tableau | REST with session |
| Taiga | open-source REST (heavy docker) |
| TalorData | SERP REST |
| Teradata | vector search via SQL driver |
| The Context Company | REST |
| ThorData | SERP REST |
| Tilores | entity-resolution GraphQL |
| Timbr | SQL semantic layer |
| Tonic Textual | PII redaction REST API |
| Traccia | OTLP |
| TrueLens | OTLP (Snowflake TruLens) |
| TypeDB | gRPC (HTTP in v3) |
| Upstash Box | REST |
| Valyu | search API |
| VAST Data | S3 + Python SDK |
| VDMS | own protocol (Intel) |
| Vector Panda | REST API |
| Vectorize | REST API; cloud only |
| Vequil | REST callback |
| Vercel | Vercel Sandbox REST |
| WaveSpeed | media generation REST |
| Webz | news REST |
| YDB | gRPC |
| Yutori | browser agents REST |
| ZeusDB | vector database (Python SDK) |

## C. OpenAI-compatible gateways (0)

.
Minimum cost (an entry in the table with the base URL through an environment variable). I have not verified the official URLs, so they would go in as "requires `$X_BASE_URL`".

## D. Tied to a language or platform (24)

| Provider | Reason |
|---|---|
| ANEForge | Apple Neural Engine, native macOS |
| any-llm | Python library (equivalent to chatmodels.Init) |
| Apple Foundation Models | on-device in Swift; no network API |
| assistant-ui | React frontend |
| Bodo DataFrames | Python library |
| CopilotKit | React frontend |
| DProvenanceKit | Python library |
| John Snow Labs | Python/Spark NLP library |
| KoNLPy | Python library for Korean NLP |
| Kuzu | embedded C++ database (would require cgo) |
| LangChain Decorators | Python syntactic sugar |
| LangChain Replay | Python library |
| langchain-arabic | Python post-processing |
| LangFair | Python bias-testing library |
| Open Agent Spec | agent definition format (Python) |
| OpenDataLoader PDF | Java/Python PDF library |
| oxidize-pdf | Rust library |
| pdfmuse | local PDF library |
| Prolog | Python–SWI bridge |
| PyMuPDF4LLM | Python library |
| Sail | Spark replacement (Rust/gRPC Spark Connect) |
| Semantica | Python adapters |
| Toolbox LangChain | Python tools package |
| Whoosh | Python search engine (BM25 already exists) |

## E. Community and niche (221)

Agent packages for governance, identity, auditing, guardrails, x402 payments, crypto, niche data (tennis, soccer, EDGAR, DEX),
startup memories and proxies. No established product or stable API: 1Claw, AACP, Abso, Adeu, Ads4GPTs, Agent Action Capsule, Agent Evidence, Agent Traffic Lab, AgentGate, Agentic SpendGuard, AgenticEmail, AgentLair, AgentLine, AgentMail, AgentOps Cockpit, AgentPhone, AgentSeal, agentstate, AgentSystems, AI Identity, Aidress, AlexEvidenceVerifier, Algenta, AlphaAI, Ampersend, AMUVectorStore, AntiBrow, AnyAPI, AproxPay, Arcmira, ARO, Asqav, Asyntai, Atomic Mail, ATR Guardrail, Attenu Guard, Ausca, Axiora, AxioRank, Bastion Prompt Protection, Bidda, Blindfold, Blopus, BowmarkToolkit, Capslane, Ceki, ChronoVec, Clearsigned, cloro, Coalent, comply54, Cosmergon, CrustAPI, CTRLRun, CVFile, DeepKeep, Delegare, DexPaprika, Discord Shikenso, Distil, DNS Doctor, DomPruner, Duel Agents, DynamicToolSelectorMiddleware, EnConvert, ERC-8004, ERC20, FeedMyAgent, FiGuard, Fizzl, FlashDataToolkit, FlatmarkLoader, FlipCoin, FMP Data, Foliant, Football Charts, FoundryNet, FoxNose, GAIP, Galaxia, GateCore, Gel, GoAT, GoodSender, Graceful Fail, GreenCalculus, Haldir, Hashlock, Highflame, HighSNR, HippoDid, Hlido, HuangtingFlux, HWP/HWPX, Hydrafetch, ICTContact, ICTExam, ICTFax, ICTPBX, iGPT, iLoveVideoEditor, Inferrail, Inspeximus, Instanode, Interven, ISNAD, Kalibr, Kerq, Kyvvu, LatAm Synth, LexMex, Lians, Lithtrix, Live Tennis API, LongProbe, Lyrenth, m3-memory, Machine Library, MadeOnSol, MartToolkit, MaskFlow, MemoryRouter, Memstate AI, MeragptDecider, MinnsDB, mnki (Agent Trust), Modexia, Motorhead, Ned Watch, NeuralTrust, Nexus Shield, NodeProxy, nono, Nuggets, OMEM, Opedd, OpenBox, OpenDMA, OpenGradient, OpenMemory, OpenTTT, OpenZoo, OrcaCallbackHandler, PaySafe, Perseus Vault, Pexafy, PIIGhost, Pinchwork, Pipeshift, Plasmate, Polaris, PolarisAIDataInsight, POP, PQS, pr402, Pramagent, Privy, prompt-shield, PromptfirewallGuard, ProxyClaw, ProxyHat, Pushary, RE-call, RelayShield, Remembrall, ReplyLayer, Reserp, Robocorp, Runcycles, RustChain, SafePrompt, SchemagateRetriever, ScoutWyze Compute, seekrit, Shale Protocol, ShannonBase, SibFly, SidClaw, Signatrust, Signet, Simplepages, SkillLite, Skim, Soul, Sourcey, Strale, Sulci Cache, SuperColony, SuperLocalMemory, SupraWall, Synmerco, TanodToolkit, TealTiger, TempGuru, Tendem, TensorLake, Tenuo, Tessera, Tokonomics, TokPortal, Toolstem, Truth Bear, TypeSafe, UnDatasIO, UniRate, Uniswap, UploadPostToolkit, URLpipe, Valthera, Vaultak, vecr-compress, Verifly, Veritensor, Voxell, Web Metadata Extractor, Work Ledger, x402, xaidr, Xpoz, YantrikDB, YeagerAI.

## Real gaps the list reveals (independent of the names)

1. **S3/object client** (Tigris, XNS, AWS S3, GCS, Azure Blob): loader, `checkpoint.Saver` and byte store.
2. **Native PDF/Office loader** (PyMuPDF, MinerU, oxidize-pdf, OpenDataLoader): today only via Unstructured.
3. **Graph retriever** (Graph RAG) and **AG-UI adapter** for `StateGraph` streaming.
4. **Real code sandbox** (Daytona, E2B, Runloop, Kubernetes): today only `LocalShell`.
5. **Google/AWS/Azure services beyond the model**: Firestore (emulator), GCS, Vertex Vector Search, Bedrock Knowledge Bases, Kendra, Azure AI Search.

## Suggested order

1. S3 client + tests with MinIO (unlocks Tigris, XNS, S3).
2. ClickHouse, CrateDB, CockroachDB, Oracle, ArcadeDB, Solr, Infinispan, SpiceDB (all with Docker).
3. Graph RAG and AG-UI (offline).
4. Group B by the priority above, each one with the caveat "no real test".
