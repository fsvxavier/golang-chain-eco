# Análise dos 368 provedores restantes

Fonte dos nomes e descrições: cartões de <https://docs.langchain.com/oss/python/integrations/providers/all_providers>.
Critério de "não é de terceiros": produto de empresa estabelecida ou projeto aberto conhecido, com API/protocolo público e documentado, e
que não é apenas um complemento comunitário para agentes (governança, identidade, pagamentos x402, cripto, memórias de startups).
Critério de "pode ser implementado": dá para escrever o cliente em Go com a stdlib e **provar** que funciona, seja contra o serviço real
(Docker, emulador) ou, no mínimo, contra um servidor de teste que reproduz o contrato público.

| Grupo | Qtde | O que significa |
|---|---|---|
| **A. Implementável e testável de verdade** | 0 | serviço open source/emulador em Docker, ou adaptador offline |
| **B. Produto/vendor com API pública** | 123 | implementável, mas só com teste `httptest`, ou exige licença/conta/driver |
| **C. Gateways OpenAI-compatíveis** | 0 | 1 linha na tabela `OpenAICompatible`, com `$<PROVEDOR>_BASE_URL` (URL oficial não verificada) |
| **D. Presos a uma linguagem/plataforma** | 24 | biblioteca Python/Rust/Swift ou frontend; não há o que portar |
| **E. Comunitários/nicho** | 221 | descartados |

## A. Implementáveis e testáveis (0 pendentes; 16 já implementados: ArcadeDB, CrateDB, chDB, CockroachDB, Oracle AI Database, Infinispan, Opensolr, Memgraph, Couchbase, Tigris, XNS, SpiceDB, Jenkins, Kubernetes, AG-UI Protocol, Graph RAG)

| Provedor | Esforço | Como testar | Observação |
|---|---|---|---|

Esforço: S = horas, M = meio dia, L = dia ou mais. Três deles destravam vários cartões de uma vez: o **cliente S3** (Tigris, XNS e o próprio
AWS S3, Lambda e SQS por LocalStack), o **ClickHouse** (cobre chDB) e o **driver SQL de teste** (Oracle, CockroachDB).

## B. Produtos com API pública, sem teste real possível aqui (123 pendentes; 12 implementados com teste de contrato: Replicate, IBM, Microsoft, Salesforce, Vectara, Zotero, YouTube, Plivo, Telnyx, Permit, Oxylabs, Perigon)

Implementáveis, mas eu só conseguiria validar contra um servidor de teste escrito por mim a partir do contrato; o risco é a API real divergir.
Prioridade sugerida (APIs maduras e documentadas): IBM watsonx, Microsoft (Azure AI Search), Replicate, Vectara, Zotero, YouTube Data API, Plivo/Telnyx,
Permit, Oxylabs, Parallel, Perigon e os sandboxes Daytona, E2B e Runloop.

| Provedor | Contrato / limitação |
|---|---|
| Actian VectorAI DB | banco vetorial embutido/nuvem |
| Aerospike | protocolo binário próprio |
| AgentQL | consulta em linguagem natural |
| AgentRAM | REST |
| Airbyte | API de conexões/jobs |
| Alchemyst AI | REST |
| Anakin | scraping REST |
| Anchor Browser | sessões + CDP |
| Baponi | REST |
| Box | REST + OAuth (loader) |
| Browserbase | sessões + CDP (precisa de cliente CDP) |
| CAMB AI | TTS/dublagem REST |
| Capsule | WebAssembly (wazero em Go seria o caminho) |
| Cognee | open source, REST /api/v1 |
| Confident AI | OTLP/REST (DeepEval) |
| Context | REST |
| Context.dev | scraping/extração REST |
| Contextual | rerank/geração (api.contextual.ai) |
| CosVectors | Tencent COS Vectors, API assinada |
| Crawleo | search/crawl REST |
| CRW | compatível com Firecrawl: basta apontar a URL base do Firecrawl |
| Dappier | AI data API |
| Daytona | REST + toolbox API |
| DeepLake | Activeloop; formato próprio, SDK Python |
| Dell | Dell AI Factory/Enterprise Hub (API a confirmar) |
| Dewey | API REST de documentos/retriever |
| Docling | docling-serve REST (/v1alpha/convert); biblioteca é Python |
| Docugami | API REST de documentos |
| E2B | REST + Connect-RPC (envd) |
| Egnyte | REST + OAuth (loader/busca) |
| Engram | REST |
| Firebolt | API HTTP + vector_search; só nuvem |
| Gandr | TTS REST |
| GetYouTubeTranscript | transcrições REST |
| GoodMem | REST |
| GraphSignal | SDK/API |
| GridGain | Ignite thin client binário (checkpointer/store/cache) |
| Hindsight | open source, REST |
| HyperBrowser | scrape/sessões REST |
| Infino | API REST |
| Isaacus | modelos jurídicos (embedding/classificação) via API |
| Islo | REST |
| Kavel | geração de imagem REST |
| Keenable | search API |
| Keiro | search API |
| Kinetica | REST/SQL com vetores; imagem exige licença |
| LambdaDB | API REST serverless |
| Leap0 | REST |
| Lindorm | Alibaba Cloud multi-modelo (SQL/ES compatível) |
| Lizard | REST |
| Log10 | API REST |
| Magic Hour | geração de mídia REST |
| Mengram | REST |
| Metal | descontinuado |
| MindsDB | open source; SQL/HTTP, imagem docker disponível |
| MinerU | API REST do serviço; biblioteca é Python |
| Mixpeek | API REST multimodal; só nuvem |
| Modal | SDK gRPC |
| Moorcheh | API REST; só nuvem |
| MrScraper | scraping REST |
| NewsCatcher | notícias REST |
| Nia | REST |
| Nimble | web data REST |
| Notte | sessões + CDP |
| Noveum | traces REST |
| OceanBase | protocolo MySQL + cosine_distance; imagem pesada |
| OCI | Generative AI com requisições assinadas (RSA) |
| Octen | search API |
| OpenSandbox | servidor open source (API a confirmar) |
| Opsen | REST |
| Parallel | Search/Extract API |
| PlainId | PDP REST |
| Play Store | scraping não oficial |
| Polign | API REST |
| QuanticData | SERP/crawl REST |
| Querit | search API |
| Respan | gateway + traces |
| Rostam | binário próprio (vetor + KV) |
| Runloop | REST (devboxes) |
| Runtime | microVMs Firecracker, REST |
| SAP | HANA Cloud (driver go-hdb) e Generative AI Hub |
| Scavio | search API |
| ScrapeGraph | smartscraper REST |
| Scrapeless | scraping REST |
| ScyllaDB | CQL binário; busca vetorial só em versões recentes/Cloud |
| SereneDB | compatível com PostgreSQL (pgvector provável; sem imagem verificada) |
| SERPdive | search API |
| Serpex | search API |
| SerpKite | search API |
| SingleStore | protocolo MySQL + DOT_PRODUCT; imagem exige licença |
| SnapRender | screenshots REST |
| Soniox | STT REST/WebSocket |
| Spanlens | callback + REST |
| Spidra | scraping REST |
| Stardog | SPARQL HTTP; licença comercial |
| Supadata | transcrições REST |
| Superserve | REST |
| Synap | REST |
| Tableau | REST com sessão |
| Taiga | REST open source (docker pesado) |
| TalorData | SERP REST |
| Teradata | vector search por driver SQL |
| The Context Company | REST |
| ThorData | SERP REST |
| Tilores | GraphQL de entity resolution |
| Timbr | camada semântica SQL |
| Tonic Textual | API REST de redação de PII |
| Traccia | OTLP |
| TrueLens | OTLP (Snowflake TruLens) |
| TypeDB | gRPC (HTTP na v3) |
| Upstash Box | REST |
| Valyu | search API |
| VAST Data | S3 + SDK Python |
| VDMS | protocolo próprio (Intel) |
| Vector Panda | API REST |
| Vectorize | API REST; só nuvem |
| Vequil | callback REST |
| Vercel | Vercel Sandbox REST |
| WaveSpeed | geração de mídia REST |
| Webz | notícias REST |
| YDB | gRPC |
| Yutori | browser agents REST |
| ZeusDB | banco vetorial (SDK Python) |

## C. Gateways OpenAI-compatíveis (0)

.
Custo mínimo (entrada na tabela com base URL por variável de ambiente). Não verifiquei as URLs oficiais, por isso entrariam como "exige `$X_BASE_URL`".

## D. Presos a linguagem ou plataforma (24)

| Provedor | Motivo |
|---|---|
| ANEForge | Apple Neural Engine, nativo macOS |
| any-llm | biblioteca Python (equivale a chatmodels.Init) |
| Apple Foundation Models | on-device em Swift; sem API de rede |
| assistant-ui | frontend React |
| Bodo DataFrames | biblioteca Python |
| CopilotKit | frontend React |
| DProvenanceKit | biblioteca Python |
| John Snow Labs | biblioteca Python/Spark NLP |
| KoNLPy | biblioteca Python de NLP coreano |
| Kuzu | banco embutido C++ (exigiria cgo) |
| LangChain Decorators | açúcar sintático Python |
| LangChain Replay | biblioteca Python |
| langchain-arabic | pós-processamento Python |
| LangFair | biblioteca Python de testes de viés |
| Open Agent Spec | formato de definição de agentes (Python) |
| OpenDataLoader PDF | biblioteca Java/Python de PDF |
| oxidize-pdf | biblioteca Rust |
| pdfmuse | biblioteca local de PDF |
| Prolog | ponte Python-SWI |
| PyMuPDF4LLM | biblioteca Python |
| Sail | substituto do Spark (Rust/gRPC Spark Connect) |
| Semantica | adaptadores Python |
| Toolbox LangChain | pacote Python de ferramentas |
| Whoosh | motor de busca Python (já há BM25) |

## E. Comunitários e de nicho (221)

Pacotes de agentes para governança, identidade, auditoria, guardrails, pagamentos x402, cripto, dados de nicho (tênis, futebol, EDGAR, DEX),
memórias de startups e proxies. Sem produto estabelecido ou API estável: 1Claw, AACP, Abso, Adeu, Ads4GPTs, Agent Action Capsule, Agent Evidence, Agent Traffic Lab, AgentGate, Agentic SpendGuard, AgenticEmail, AgentLair, AgentLine, AgentMail, AgentOps Cockpit, AgentPhone, AgentSeal, agentstate, AgentSystems, AI Identity, Aidress, AlexEvidenceVerifier, Algenta, AlphaAI, Ampersend, AMUVectorStore, AntiBrow, AnyAPI, AproxPay, Arcmira, ARO, Asqav, Asyntai, Atomic Mail, ATR Guardrail, Attenu Guard, Ausca, Axiora, AxioRank, Bastion Prompt Protection, Bidda, Blindfold, Blopus, BowmarkToolkit, Capslane, Ceki, ChronoVec, Clearsigned, cloro, Coalent, comply54, Cosmergon, CrustAPI, CTRLRun, CVFile, DeepKeep, Delegare, DexPaprika, Discord Shikenso, Distil, DNS Doctor, DomPruner, Duel Agents, DynamicToolSelectorMiddleware, EnConvert, ERC-8004, ERC20, FeedMyAgent, FiGuard, Fizzl, FlashDataToolkit, FlatmarkLoader, FlipCoin, FMP Data, Foliant, Football Charts, FoundryNet, FoxNose, GAIP, Galaxia, GateCore, Gel, GoAT, GoodSender, Graceful Fail, GreenCalculus, Haldir, Hashlock, Highflame, HighSNR, HippoDid, Hlido, HuangtingFlux, HWP/HWPX, Hydrafetch, ICTContact, ICTExam, ICTFax, ICTPBX, iGPT, iLoveVideoEditor, Inferrail, Inspeximus, Instanode, Interven, ISNAD, Kalibr, Kerq, Kyvvu, LatAm Synth, LexMex, Lians, Lithtrix, Live Tennis API, LongProbe, Lyrenth, m3-memory, Machine Library, MadeOnSol, MartToolkit, MaskFlow, MemoryRouter, Memstate AI, MeragptDecider, MinnsDB, mnki (Agent Trust), Modexia, Motorhead, Ned Watch, NeuralTrust, Nexus Shield, NodeProxy, nono, Nuggets, OMEM, Opedd, OpenBox, OpenDMA, OpenGradient, OpenMemory, OpenTTT, OpenZoo, OrcaCallbackHandler, PaySafe, Perseus Vault, Pexafy, PIIGhost, Pinchwork, Pipeshift, Plasmate, Polaris, PolarisAIDataInsight, POP, PQS, pr402, Pramagent, Privy, prompt-shield, PromptfirewallGuard, ProxyClaw, ProxyHat, Pushary, RE-call, RelayShield, Remembrall, ReplyLayer, Reserp, Robocorp, Runcycles, RustChain, SafePrompt, SchemagateRetriever, ScoutWyze Compute, seekrit, Shale Protocol, ShannonBase, SibFly, SidClaw, Signatrust, Signet, Simplepages, SkillLite, Skim, Soul, Sourcey, Strale, Sulci Cache, SuperColony, SuperLocalMemory, SupraWall, Synmerco, TanodToolkit, TealTiger, TempGuru, Tendem, TensorLake, Tenuo, Tessera, Tokonomics, TokPortal, Toolstem, Truth Bear, TypeSafe, UnDatasIO, UniRate, Uniswap, UploadPostToolkit, URLpipe, Valthera, Vaultak, vecr-compress, Verifly, Veritensor, Voxell, Web Metadata Extractor, Work Ledger, x402, xaidr, Xpoz, YantrikDB, YeagerAI.

## Lacunas reais que a lista revela (independentes dos nomes)

1. **Cliente S3/objetos** (Tigris, XNS, AWS S3, GCS, Azure Blob): loader, `checkpoint.Saver` e byte store.
2. **Loader de PDF/Office nativo** (PyMuPDF, MinerU, oxidize-pdf, OpenDataLoader): hoje só via Unstructured.
3. **Retriever de grafo** (Graph RAG) e **adaptador AG-UI** para o streaming do `StateGraph`.
4. **Sandbox de código real** (Daytona, E2B, Runloop, Kubernetes): hoje só `LocalShell`.
5. **Serviços Google/AWS/Azure além do modelo**: Firestore (emulador), GCS, Vertex Vector Search, Bedrock Knowledge Bases, Kendra, Azure AI Search.

## Ordem sugerida

1. Cliente S3 + testes com MinIO (destrava Tigris, XNS, S3).
2. ClickHouse, CrateDB, CockroachDB, Oracle, ArcadeDB, Solr, Infinispan, SpiceDB (todos com Docker).
3. Graph RAG e AG-UI (offline).
4. Grupo B pela prioridade acima, cada um com a ressalva "sem teste real".
