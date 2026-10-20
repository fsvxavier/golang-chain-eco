# Análisis de los 368 proveedores restantes

[English](../en-US/PROVIDERS-ANALISE.md) | [Português](../pt-BR/PROVIDERS-ANALISE.md) | **Español**

Fuente de los nombres y descripciones: tarjetas de <https://docs.langchain.com/oss/python/integrations/providers/all_providers>.
Criterio de "no es de terceros": producto de una empresa establecida o proyecto abierto conocido, con API/protocolo público y documentado, y
que no es solo un complemento comunitario para agentes (gobernanza, identidad, pagos x402, cripto, memorias de startups).
Criterio de "se puede implementar": se puede escribir el cliente en Go con la stdlib y **demostrar** que funciona, sea contra el servicio real
(Docker, emulador) o, como mínimo, contra un servidor de prueba que reproduce el contrato público.

| Grupo | Cant. | Qué significa |
|---|---|---|
| **A. Implementable y probable de verdad** | 0 | servicio open source/emulador en Docker, o adaptador offline |
| **B. Producto/vendor con API pública** | 123 | implementable, pero solo con prueba `httptest`, o exige licencia/cuenta/driver |
| **C. Gateways compatibles con OpenAI** | 0 | 1 línea en la tabla `OpenAICompatible`, con `$<PROVEEDOR>_BASE_URL` (URL oficial no verificada) |
| **D. Atados a un lenguaje/plataforma** | 24 | biblioteca Python/Rust/Swift o frontend; no hay nada que portar |
| **E. Comunitarios/nicho** | 221 | descartados |

## A. Implementables y probables (0 pendientes; 16 ya implementados: ArcadeDB, CrateDB, chDB, CockroachDB, Oracle AI Database, Infinispan, Opensolr, Memgraph, Couchbase, Tigris, XNS, SpiceDB, Jenkins, Kubernetes, AG-UI Protocol, Graph RAG)

| Proveedor | Esfuerzo | Cómo probar | Observación |
|---|---|---|---|

Esfuerzo: S = horas, M = medio día, L = un día o más. Tres de ellos desbloquean varias tarjetas de una vez: el **cliente S3** (Tigris, XNS y el propio
AWS S3, Lambda y SQS mediante LocalStack), **ClickHouse** (cubre chDB) y el **driver SQL de prueba** (Oracle, CockroachDB).

## B. Productos con API pública, sin prueba real posible aquí (123 pendientes; 12 implementados con prueba de contrato: Replicate, IBM, Microsoft, Salesforce, Vectara, Zotero, YouTube, Plivo, Telnyx, Permit, Oxylabs, Perigon)

Implementables, pero solo podría validarlos contra un servidor de prueba escrito por mí a partir del contrato; el riesgo es que la API real diverja.
Prioridad sugerida (APIs maduras y documentadas): IBM watsonx, Microsoft (Azure AI Search), Replicate, Vectara, Zotero, YouTube Data API, Plivo/Telnyx,
Permit, Oxylabs, Parallel, Perigon y los sandboxes Daytona, E2B y Runloop.

| Proveedor | Contrato / limitación |
|---|---|
| Actian VectorAI DB | base de datos vectorial embebida/en la nube |
| Aerospike | protocolo binario propio |
| AgentQL | consulta en lenguaje natural |
| AgentRAM | REST |
| Airbyte | API de conexiones/jobs |
| Alchemyst AI | REST |
| Anakin | scraping REST |
| Anchor Browser | sesiones + CDP |
| Baponi | REST |
| Box | REST + OAuth (loader) |
| Browserbase | sesiones + CDP (necesita un cliente CDP) |
| CAMB AI | REST de TTS/doblaje |
| Capsule | WebAssembly (wazero en Go sería el camino) |
| Cognee | open source, REST /api/v1 |
| Confident AI | OTLP/REST (DeepEval) |
| Context | REST |
| Context.dev | REST de scraping/extracción |
| Contextual | rerank/generación (api.contextual.ai) |
| CosVectors | Tencent COS Vectors, API firmada |
| Crawleo | REST de búsqueda/crawl |
| CRW | compatible con Firecrawl: basta con apuntar a la URL base de Firecrawl |
| Dappier | API de datos de IA |
| Daytona | REST + API de toolbox |
| DeepLake | Activeloop; formato propio, SDK de Python |
| Dell | Dell AI Factory/Enterprise Hub (API por confirmar) |
| Dewey | API REST de documentos/retriever |
| Docling | docling-serve REST (/v1alpha/convert); la biblioteca es Python |
| Docugami | API REST de documentos |
| E2B | REST + Connect-RPC (envd) |
| Egnyte | REST + OAuth (loader/búsqueda) |
| Engram | REST |
| Firebolt | API HTTP + vector_search; solo nube |
| Gandr | REST de TTS |
| GetYouTubeTranscript | transcripciones REST |
| GoodMem | REST |
| GraphSignal | SDK/API |
| GridGain | cliente thin binario de Ignite (checkpointer/store/caché) |
| Hindsight | open source, REST |
| HyperBrowser | REST de scrape/sesiones |
| Infino | API REST |
| Isaacus | modelos jurídicos (embedding/clasificación) vía API |
| Islo | REST |
| Kavel | REST de generación de imágenes |
| Keenable | API de búsqueda |
| Keiro | API de búsqueda |
| Kinetica | REST/SQL con vectores; la imagen exige licencia |
| LambdaDB | API REST serverless |
| Leap0 | REST |
| Lindorm | multimodelo de Alibaba Cloud (SQL/ES compatible) |
| Lizard | REST |
| Log10 | API REST |
| Magic Hour | REST de generación de medios |
| Mengram | REST |
| Metal | descontinuado |
| MindsDB | open source; SQL/HTTP, imagen docker disponible |
| MinerU | API REST del servicio; la biblioteca es Python |
| Mixpeek | API REST multimodal; solo nube |
| Modal | SDK gRPC |
| Moorcheh | API REST; solo nube |
| MrScraper | REST de scraping |
| NewsCatcher | REST de noticias |
| Nia | REST |
| Nimble | REST de datos web |
| Notte | sesiones + CDP |
| Noveum | REST de traces |
| OceanBase | protocolo MySQL + cosine_distance; imagen pesada |
| OCI | Generative AI con solicitudes firmadas (RSA) |
| Octen | API de búsqueda |
| OpenSandbox | servidor open source (API por confirmar) |
| Opsen | REST |
| Parallel | API de Search/Extract |
| PlainId | PDP REST |
| Play Store | scraping no oficial |
| Polign | API REST |
| QuanticData | REST de SERP/crawl |
| Querit | API de búsqueda |
| Respan | gateway + traces |
| Rostam | binario propio (vector + KV) |
| Runloop | REST (devboxes) |
| Runtime | microVMs Firecracker, REST |
| SAP | HANA Cloud (driver go-hdb) y Generative AI Hub |
| Scavio | API de búsqueda |
| ScrapeGraph | REST de smartscraper |
| Scrapeless | REST de scraping |
| ScyllaDB | CQL binario; búsqueda vectorial solo en versiones recientes/Cloud |
| SereneDB | compatible con PostgreSQL (pgvector probable; sin imagen verificada) |
| SERPdive | API de búsqueda |
| Serpex | API de búsqueda |
| SerpKite | API de búsqueda |
| SingleStore | protocolo MySQL + DOT_PRODUCT; la imagen exige licencia |
| SnapRender | REST de capturas de pantalla |
| Soniox | STT REST/WebSocket |
| Spanlens | callback + REST |
| Spidra | REST de scraping |
| Stardog | SPARQL HTTP; licencia comercial |
| Supadata | transcripciones REST |
| Superserve | REST |
| Synap | REST |
| Tableau | REST con sesión |
| Taiga | REST open source (docker pesado) |
| TalorData | REST de SERP |
| Teradata | vector search por driver SQL |
| The Context Company | REST |
| ThorData | REST de SERP |
| Tilores | GraphQL de entity resolution |
| Timbr | capa semántica SQL |
| Tonic Textual | API REST de redacción de PII |
| Traccia | OTLP |
| TrueLens | OTLP (TruLens de Snowflake) |
| TypeDB | gRPC (HTTP en la v3) |
| Upstash Box | REST |
| Valyu | API de búsqueda |
| VAST Data | S3 + SDK de Python |
| VDMS | protocolo propio (Intel) |
| Vector Panda | API REST |
| Vectorize | API REST; solo nube |
| Vequil | callback REST |
| Vercel | REST de Vercel Sandbox |
| WaveSpeed | REST de generación de medios |
| Webz | REST de noticias |
| YDB | gRPC |
| Yutori | agentes de navegador REST |
| ZeusDB | base de datos vectorial (SDK de Python) |

## C. Gateways compatibles con OpenAI (0)

.
Costo mínimo (entrada en la tabla con la URL base por variable de entorno). No verifiqué las URLs oficiales, por eso entrarían como "exige `$X_BASE_URL`".

## D. Atados a un lenguaje o plataforma (24)

| Proveedor | Motivo |
|---|---|
| ANEForge | Apple Neural Engine, macOS nativo |
| any-llm | biblioteca Python (equivale a chatmodels.Init) |
| Apple Foundation Models | en el dispositivo, en Swift; sin API de red |
| assistant-ui | frontend de React |
| Bodo DataFrames | biblioteca Python |
| CopilotKit | frontend de React |
| DProvenanceKit | biblioteca Python |
| John Snow Labs | biblioteca Python/Spark NLP |
| KoNLPy | biblioteca Python de NLP coreano |
| Kuzu | base de datos embebida en C++ (requeriría cgo) |
| LangChain Decorators | azúcar sintáctico de Python |
| LangChain Replay | biblioteca Python |
| langchain-arabic | posprocesamiento en Python |
| LangFair | biblioteca Python de pruebas de sesgo |
| Open Agent Spec | formato de definición de agentes (Python) |
| OpenDataLoader PDF | biblioteca Java/Python de PDF |
| oxidize-pdf | biblioteca Rust |
| pdfmuse | biblioteca local de PDF |
| Prolog | puente Python–SWI |
| PyMuPDF4LLM | biblioteca Python |
| Sail | reemplazo de Spark (Rust/gRPC Spark Connect) |
| Semantica | adaptadores de Python |
| Toolbox LangChain | paquete Python de herramientas |
| Whoosh | motor de búsqueda en Python (ya hay BM25) |

## E. Comunitarios y de nicho (221)

Paquetes de agentes para gobernanza, identidad, auditoría, guardrails, pagos x402, cripto, datos de nicho (tenis, fútbol, EDGAR, DEX),
memorias de startups y proxies. Sin producto establecido ni API estable: 1Claw, AACP, Abso, Adeu, Ads4GPTs, Agent Action Capsule, Agent Evidence, Agent Traffic Lab, AgentGate, Agentic SpendGuard, AgenticEmail, AgentLair, AgentLine, AgentMail, AgentOps Cockpit, AgentPhone, AgentSeal, agentstate, AgentSystems, AI Identity, Aidress, AlexEvidenceVerifier, Algenta, AlphaAI, Ampersend, AMUVectorStore, AntiBrow, AnyAPI, AproxPay, Arcmira, ARO, Asqav, Asyntai, Atomic Mail, ATR Guardrail, Attenu Guard, Ausca, Axiora, AxioRank, Bastion Prompt Protection, Bidda, Blindfold, Blopus, BowmarkToolkit, Capslane, Ceki, ChronoVec, Clearsigned, cloro, Coalent, comply54, Cosmergon, CrustAPI, CTRLRun, CVFile, DeepKeep, Delegare, DexPaprika, Discord Shikenso, Distil, DNS Doctor, DomPruner, Duel Agents, DynamicToolSelectorMiddleware, EnConvert, ERC-8004, ERC20, FeedMyAgent, FiGuard, Fizzl, FlashDataToolkit, FlatmarkLoader, FlipCoin, FMP Data, Foliant, Football Charts, FoundryNet, FoxNose, GAIP, Galaxia, GateCore, Gel, GoAT, GoodSender, Graceful Fail, GreenCalculus, Haldir, Hashlock, Highflame, HighSNR, HippoDid, Hlido, HuangtingFlux, HWP/HWPX, Hydrafetch, ICTContact, ICTExam, ICTFax, ICTPBX, iGPT, iLoveVideoEditor, Inferrail, Inspeximus, Instanode, Interven, ISNAD, Kalibr, Kerq, Kyvvu, LatAm Synth, LexMex, Lians, Lithtrix, Live Tennis API, LongProbe, Lyrenth, m3-memory, Machine Library, MadeOnSol, MartToolkit, MaskFlow, MemoryRouter, Memstate AI, MeragptDecider, MinnsDB, mnki (Agent Trust), Modexia, Motorhead, Ned Watch, NeuralTrust, Nexus Shield, NodeProxy, nono, Nuggets, OMEM, Opedd, OpenBox, OpenDMA, OpenGradient, OpenMemory, OpenTTT, OpenZoo, OrcaCallbackHandler, PaySafe, Perseus Vault, Pexafy, PIIGhost, Pinchwork, Pipeshift, Plasmate, Polaris, PolarisAIDataInsight, POP, PQS, pr402, Pramagent, Privy, prompt-shield, PromptfirewallGuard, ProxyClaw, ProxyHat, Pushary, RE-call, RelayShield, Remembrall, ReplyLayer, Reserp, Robocorp, Runcycles, RustChain, SafePrompt, SchemagateRetriever, ScoutWyze Compute, seekrit, Shale Protocol, ShannonBase, SibFly, SidClaw, Signatrust, Signet, Simplepages, SkillLite, Skim, Soul, Sourcey, Strale, Sulci Cache, SuperColony, SuperLocalMemory, SupraWall, Synmerco, TanodToolkit, TealTiger, TempGuru, Tendem, TensorLake, Tenuo, Tessera, Tokonomics, TokPortal, Toolstem, Truth Bear, TypeSafe, UnDatasIO, UniRate, Uniswap, UploadPostToolkit, URLpipe, Valthera, Vaultak, vecr-compress, Verifly, Veritensor, Voxell, Web Metadata Extractor, Work Ledger, x402, xaidr, Xpoz, YantrikDB, YeagerAI.

## Lagunas reales que revela la lista (independientes de los nombres)

1. **Cliente S3/objetos** (Tigris, XNS, AWS S3, GCS, Azure Blob): loader, `checkpoint.Saver` y store de bytes.
2. **Loader de PDF/Office nativo** (PyMuPDF, MinerU, oxidize-pdf, OpenDataLoader): hoy solo vía Unstructured.
3. **Retriever de grafo** (Graph RAG) y **adaptador AG-UI** para el streaming del `StateGraph`.
4. **Sandbox de código real** (Daytona, E2B, Runloop, Kubernetes): hoy solo `LocalShell`.
5. **Servicios de Google/AWS/Azure más allá del modelo**: Firestore (emulador), GCS, Vertex Vector Search, Bedrock Knowledge Bases, Kendra, Azure AI Search.

## Orden sugerido

1. Cliente S3 + pruebas con MinIO (desbloquea Tigris, XNS, S3).
2. ClickHouse, CrateDB, CockroachDB, Oracle, ArcadeDB, Solr, Infinispan, SpiceDB (todos con Docker).
3. Graph RAG y AG-UI (offline).
4. Grupo B por la prioridad de arriba, cada uno con la salvedad "sin prueba real".
