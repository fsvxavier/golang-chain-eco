# RAG, CAG e KAG Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar RAG, CAG e KAG agnósticos e completos (núcleo, roteador, exemplos offline, manual) e, em seguida, integrações (tools, MCP, CLI, adapters de grafo Neo4j e Memgraph).

**Architecture:** Pacotes novos `knowledge` (contrato comum, roteador), `rag`, `cag`, `kag` compõem o que já existe em `core/` e `integrations/`. Cada técnica implementa `knowledge.Answerer`. O modelo, o embedder, o vector store e o grafo são injetados por interface. Fase 1 = núcleo + exemplos + manual; fase 2 = integrações.

**Tech Stack:** Go 1.27, só stdlib em código de biblioteca. Testes com `testing` e `httptest`.

**Spec:** [DESIGN-RAG-CAG-KAG.md](../../../DESIGN-RAG-CAG-KAG.md)

## Global Constraints

- Módulo `github.com/fsvxavier/golang-chain-eco`, Go 1.27; **nenhum pacote de produção importa módulo de terceiros**.
- Exemplos rodam offline com modelo fake (`demo.Smart`/`demo.Model`); provedor real só com `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`.
- Testes de serviço real pulam sozinhos sem a variável de ambiente (padrão `internal/conformance`).
- Docs e README em pt-BR; ao adicionar pacote ou exemplo atualizar mapa e contagens em `README.md`, `docs/ARCHITECTURE.md`, `docs/manual/README.md`, `examples/README.md`.
- Schemas de tool validam listas como `[]any`; ids de tool call únicos na conversa.
- Mudanças em tipos existentes são **aditivas** (não quebrar API pública).
- Verificação final de cada task: `go build ./... && go vet ./... && go test -race -count=1 <pacote>`.
- Commits terminam com `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`; não fazer push.

## Review Focus

1. Grafo com ciclo (A→B→A): a travessia termina e não repete nós (Task 6, 7).
2. Tipo de relação ou rótulo vindo do LLM com caracteres Cypher (`` ` ``, `;`, espaço): rejeitado ou sanitizado, nunca concatenado cru (Task 12, 13, e normalização na Task 6).
3. Extrator de LLM devolve JSON inválido, triplas vazias ou duplicadas: erro claro ou descarte, sem pânico nem entidade vazia (Task 6).
4. Pergunta vazia, corpus vazio, `k` maior que o acervo: erro ou resposta vazia coerente, sem pânico (Tasks 3, 4, 7).
5. Uso concorrente (`Index` + `Answer` em goroutines): passa em `-race` (Tasks 3, 5).

---

# Fase 1: núcleo, exemplos e manual

### Task 1: Campos de cache em `messages.Usage`

**Files:**
- Modify: `core/messages/messages.go` (struct `Usage` ~l.69, `Usage.Add` ~l.76)
- Modify: `providers/anthropic/anthropic.go` (`apiUsage.toUsage` ~l.188)
- Modify: `providers/openai/openai.go` (`usage`, `toUsage` ~l.264)
- Test: `core/messages/messages_test.go`, `providers/anthropic/anthropic_test.go`, `providers/openai/openai_test.go`

**Interfaces:**
- Produces: `messages.Usage{InputTokens, OutputTokens, TotalTokens int; CacheReadTokens int \`json:"cache_read_tokens,omitempty"\`; CacheCreationTokens int \`json:"cache_creation_tokens,omitempty"\`}`. `InputTokens` continua somando tudo (compatibilidade). `Add` soma os dois campos novos.

- [ ] **Step 1: Write failing tests** — `TestUsageAddCache` (Add de `{10,5,15,CacheReadTokens:4,CacheCreationTokens:2}` com `{1,1,2,CacheReadTokens:1}` dá `CacheReadTokens==5`, `CacheCreationTokens==2`); no Anthropic, resposta `httptest` com `cache_read_input_tokens:30, cache_creation_input_tokens:20, input_tokens:5` dá `Usage.CacheReadTokens==30`, `CacheCreationTokens==20`, `InputTokens==55`; no OpenAI, `prompt_tokens_details.cached_tokens:12` dá `CacheReadTokens==12` e `InputTokens` inalterado.
- [ ] **Step 2: Run** `go test ./core/messages ./providers/anthropic ./providers/openai` — Expected: FAIL (campos inexistentes).
- [ ] **Step 3: Implement** os campos e atualizar `Add`, `anthropic.toUsage`, `openai.usage`/`toUsage`. Atenção: `Add` hoje usa literal posicional; trocar por literal nomeado.
- [ ] **Step 4: Run** os mesmos pacotes mais `go build ./...` — Expected: PASS (nenhum outro código quebra).
- [ ] **Step 5: Commit** `feat(messages): cache token counters in Usage`.

### Task 2: Pacote `knowledge`: contrato comum

**Files:**
- Create: `knowledge/knowledge.go` (tipos, `Answerer`, `AsRunnable`, `SourcesFromDocs`), `knowledge/tokens.go` (contador), `knowledge/prompt.go` (prompt com citações)
- Test: `knowledge/knowledge_test.go`

**Interfaces:**
- Produces (exatos):
  - `type Question struct { Text string; History []messages.Message; Filter vectorstores.Filter; Force string }` (`Force` = `""|"rag"|"cag"|"kag"`)
  - `type Source struct { ID, Content string; Metadata map[string]any; Score float64 }`
  - `type Step struct { Name, Detail string }`
  - `type Answer struct { Text string; Sources []Source; Strategy string; Usage *messages.Usage; Trace []Step }`
  - `type Answerer interface { Answer(ctx context.Context, q Question) (Answer, error) }`
  - `func AsRunnable(a Answerer) runnables.Runnable[Question, Answer]` (via `runnables.Lambda`)
  - `func SourcesFromDocs(docs []documents.Document) []Source` (Score vem de `metadata["score"]` quando float64)
  - `type TokenCounter interface { Count(text string) int }`; `type Heuristic struct{ CharsPerToken int }` (default 4, conta runas, arredonda para cima; `""` → 0)
  - `func Prompt(system string, ctxDocs []Source, q Question) []messages.Message`: monta `System` (instrução + trechos numerados `[1]`, `[2]`) + `History` + `Human(q.Text)`; `DefaultSystem` pede resposta só com base no contexto e citação `[n]`.
  - `func Errorf`-free: erro de pergunta vazia é `knowledge.ErrEmptyQuestion` (`errors.New`).

- [ ] **Step 1: Write failing tests** — `TestHeuristic` (`Heuristic{}.Count("abcdefgh")==2`, `"ação"` conta 4 runas → 1, `""`→0, `CharsPerToken:2` muda o resultado); `TestPromptNumbersSources` (2 fontes geram `[1]` e `[2]` no system e última mensagem `Human` com o texto); `TestAsRunnable` (fake `Answerer` passa por `Invoke`).
- [ ] **Step 2: Run** `go test ./knowledge` — Expected: FAIL (pacote inexistente).
- [ ] **Step 3: Implement** os tipos acima nos três arquivos.
- [ ] **Step 4: Run** `go test -race ./knowledge` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(knowledge): common Answerer contract`.

### Task 3: RAG: indexador e pipeline

**Files:**
- Create: `rag/indexer.go`, `rag/pipeline.go`
- Test: `rag/rag_test.go`

**Interfaces:**
- Consumes: `knowledge.*` (Task 2); `retrievers.FromVectorStore`, `retrievers.Ensemble`, `retrievers.MultiQuery`; `retrieval.NewBM25`, `retrieval.WithRerank`, `retrieval.Reranker`; `vectorstores.VectorStore`; `textsplitters.Splitter`.
- Produces:
  - `type Indexer struct{...}`; `func NewIndexer(store vectorstores.VectorStore, sp textsplitters.Splitter) *Indexer` (nil `sp` → `textsplitters.NewRecursive(800, 100)`)
  - `type IndexStats struct{ Added, Skipped, Removed int }`
  - `func (i *Indexer) Index(ctx context.Context, l documents.Loader) (IndexStats, error)`: por documento calcula sha256 do conteúdo; fonte inalterada (mesmo `metadata["source"]` e hash) é ignorada; alterada remove os chunks antigos (`store.Delete`) e reindexa. Chunks recebem `source`, `chunk`, `content_hash`. Estado de hash só vive no processo (ponytail: sem persistência entre execuções).
  - `func (i *Indexer) Documents() []documents.Document` (todos os chunks, para montar o BM25)
  - `type Pipeline struct { Store vectorstores.VectorStore; Docs []documents.Document; Model models.ChatModel; K int; FetchK int; Hybrid bool; MultiQueries int; Reranker retrieval.Reranker; System string; MaxContextTokens int; Counter knowledge.TokenCounter }`; `func (p *Pipeline) Answer(ctx, q) (knowledge.Answer, error)` implementa `knowledge.Answerer`; `func (p *Pipeline) Retrieve(ctx, q knowledge.Question) ([]knowledge.Source, error)`.
  - Defaults: `K=4`; `Hybrid` combina vetor + BM25 sobre `Docs` por `retrievers.Ensemble`; `MaxContextTokens` corta trechos do fim até caber (padrão sem limite).

- [ ] **Step 1: Write failing tests** (embeddings `embeddings.Fake{Dim:128}`, `vectorstores.NewMemory`, `models.Fake` via `Func` que devolve `AI("ok [1]")`): `TestIndexIncremental` (segunda chamada com mesma fonte → `Skipped==n, Added==0`; fonte alterada → `Removed>0`); `TestAnswerReturnsSources` (`len(ans.Sources)==K`, `ans.Strategy=="rag"`, `Usage` repassada do modelo); `TestHybridFindsKeywordOnlyHit` (documento com termo raro, embedder `Fake` não o ranqueia no topo, BM25 sim: presente em `Hybrid:true`, ausente em `false`); `TestMaxContextTokensTrims`; `TestEmptyQuestion` (`ErrEmptyQuestion`); `TestEmptyStore` (resposta sem fontes, sem pânico); `TestConcurrentIndexAnswer` sob `-race`.
- [ ] **Step 2: Run** `go test ./rag` — Expected: FAIL.
- [ ] **Step 3: Implement** `Indexer` e `Pipeline`. Proteger o mapa de hashes com `sync.Mutex`. Filtro de `q.Filter` vai ao retriever vetorial; no BM25 aplicar o filtro na pós-filtragem.
- [ ] **Step 4: Run** `go test -race ./rag` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(rag): incremental indexer and hybrid pipeline`.

### Task 4: CAG: corpus em prefixo cacheável

**Files:**
- Create: `cag/corpus.go`, `cag/cag.go`
- Test: `cag/cag_test.go`

**Interfaces:**
- Consumes: `knowledge.*`, `knowledge.TokenCounter`, `rag.Pipeline` (apenas como fallback via `knowledge.Answerer`), `documents.Loader`, `messages.ContentPart.Extra`.
- Produces:
  - `type Corpus struct{...}`; `func NewCorpus(docs []documents.Document) *Corpus` (ordena por `metadata["source"]` depois por `ID` e conteúdo; deterministico); `func (c *Corpus) Text() string` (blocos `<document source="...">\n...\n</document>`); `func (c *Corpus) Hash() string` (sha256 hex do `Text()`); `func (c *Corpus) Tokens(tc knowledge.TokenCounter) int`.
  - `type PrefixCacher interface { Mark(system messages.Message) messages.Message }`; `type AnthropicCache struct{}` (coloca `Parts` com um bloco `text` e `Extra{"cache_control": map[string]any{"type":"ephemeral"}}`); `type StablePrefix struct{}` (devolve a mensagem sem alteração: prefixo estável para cache automático). `func CacherFor(modelID string) PrefixCacher` (`anthropic:` → `AnthropicCache`, senão `StablePrefix`).
  - `type Engine struct { Model models.ChatModel; Corpus *Corpus; Window int; Reserve int; Counter knowledge.TokenCounter; Cacher PrefixCacher; Fallback knowledge.Answerer; System string }`; `func (e *Engine) Answer(ctx, q) (knowledge.Answer, error)`: se `Corpus.Tokens + Reserve > Window` → `Fallback.Answer` com `Trace` registrando o motivo, ou `ErrCorpusTooLarge` se `Fallback==nil`. Ordem das mensagens: `[system com corpus (marcado)] + q.History + Human(q.Text)`. `Trace` inclui `corpus_hash` e, quando `Usage.CacheReadTokens>0`, um passo `cache_hit`.
  - `func (e *Engine) SetCorpus(c *Corpus) (changed bool)`: troca o corpus; `changed` é verdadeiro se o hash mudou.
  - `var ErrCorpusTooLarge = errors.New(...)`; `Window` padrão 200000, `Reserve` padrão 4096.

- [ ] **Step 1: Write failing tests**: `TestPrefixByteStable` (duas chamadas com `History` diferente produzem a mesma `messages[0]` byte a byte e a mesma ordem mesmo com docs fornecidos em ordem diferente); `TestAnthropicCacheMarker` (`messages[0].Parts` tem `cache_control` em `Extra`); `TestStablePrefixHasNoMarker`; `TestTooLargeFallsBack` (corpus acima da janela chama o `Fallback` e `Answer.Trace` explica; sem fallback → `ErrCorpusTooLarge`); `TestSetCorpusHash` (`changed` falso para mesmo corpus, verdadeiro para outro); `TestCacheHitTrace` (modelo fake devolve `Usage{CacheReadTokens:10}` → passo `cache_hit`); `TestEmptyCorpus` (erro claro, não envia prompt vazio).
- [ ] **Step 2: Run** `go test ./cag` — Expected: FAIL.
- [ ] **Step 3: Implement** os tipos acima.
- [ ] **Step 4: Run** `go test -race ./cag` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(cag): cache-friendly corpus prefix engine`.

### Task 5: KAG: modelo de grafo e `GraphStore` em memória

**Files:**
- Create: `kag/graph.go` (tipos + interface), `kag/memory.go` (implementação), `internal/conformance/graph.go` (suíte reutilizável)
- Test: `kag/memory_test.go`

**Interfaces:**
- Produces:
  - `type Entity struct { ID, Type, Name string; Props map[string]any; Vector []float32 }`
  - `type Relation struct { From, Type, To string; Props map[string]any; Source string }` (`Source` = id/fonte do chunk)
  - `type Direction int` com `Out`, `In`, `Both`
  - `type GraphStore interface { UpsertEntities(ctx, []Entity) error; UpsertRelations(ctx, []Relation) error; Neighbors(ctx, id string, relTypes []string, dir Direction) ([]Relation, error); FindEntities(ctx, text string, vec []float32, k int) ([]Entity, error); Entity(ctx, id string) (Entity, bool, error); Path(ctx, from, to string, maxHops int) ([][]Relation, error); Delete(ctx, ids []string) error }`
  - `type Memory struct{...}`; `func NewMemory() *Memory`; `func (m *Memory) Save(path string) error`; `func LoadMemory(path string) (*Memory, error)` (JSON).
  - `FindEntities`: pontua por cosseno quando `vec` e `Entity.Vector` existem, senão por correspondência de nome normalizado (lowercase, sem acento básico, tokens); empates por ID.
  - `Path`: BFS com `visited` (termina em ciclos), devolve todos os caminhos mais curtos até `maxHops`.
  - `func NormalizeID(s string) string` (lowercase, trim, espaços→`_`).
  - Suíte: `conformance.Graph(t *testing.T, g kag.GraphStore)` (upsert, vizinhos por direção/tipo, caminho, ciclo A→B→A, delete). Importa `kag`; sem ciclo porque `kag` não importa `internal/conformance`.

- [ ] **Step 1: Write failing tests** em `kag/memory_test.go` chamando `conformance.Graph(t, NewMemory())` mais `TestPathTerminatesOnCycle`, `TestSaveLoadRoundTrip`, `TestFindEntitiesByName` e `TestConcurrentUpsertRead` (`-race`).
- [ ] **Step 2: Run** `go test ./kag` — Expected: FAIL.
- [ ] **Step 3: Implement** tipos, `Memory` (`sync.RWMutex`, mapas de entidades, de arestas de saída e de entrada) e a suíte de conformidade.
- [ ] **Step 4: Run** `go test -race ./kag ./internal/conformance` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(kag): graph model and in-memory GraphStore`.

### Task 6: KAG: extrator de triplas por LLM

**Files:**
- Create: `kag/extract.go`
- Test: `kag/extract_test.go`

**Interfaces:**
- Consumes: `models.Structured[T](m, models.MethodTool)`, `embeddings.Embedder`, `GraphStore` (Task 5), `documents.Document`.
- Produces:
  - `type Schema struct { EntityTypes, RelationTypes []string }` (vazio = livre)
  - `type Extractor struct { Model models.ChatModel; Embedder embeddings.Embedder; Schema Schema; Graph GraphStore; Method models.StructuredMethod }`
  - `func (x *Extractor) Extract(ctx context.Context, docs []documents.Document) (ExtractStats, error)`; `type ExtractStats struct{ Entities, Relations, Dropped int }`
  - Saída estruturada por chunk: `{entities:[{name,type}], relations:[{from,type,to}]}`. Pós-processamento: IDs por `NormalizeID`; relação com ponta inexistente cria a entidade; descarta vazios e duplicatas (`Dropped` conta); `Type` de relação normalizado para `[A-Z][A-Z0-9_]*` (`[^A-Za-z0-9]`→`_`, uppercase) e descartado se vazio; se `Schema.RelationTypes` não vazio, tipos fora dele são descartados. `Relation.Source` = `doc.ID` ou `metadata["source"]`. Vetores via `Embedder.EmbedDocuments` dos nomes.
  - Falha de um chunk (JSON inválido) não derruba o lote: erro agregado com `errors.Join` depois de processar os demais.

- [ ] **Step 1: Write failing tests** (modelo fake que devolve tool call JSON): `TestExtractBuildsGraph` (2 triplas → 3 entidades e 2 relações, IDs normalizados, `Source` preenchido); `TestExtractNormalizesRelationType` (`"trata com; DROP"` → `TRATA_COM_DROP`); `TestExtractDropsDuplicatesAndEmpty`; `TestExtractSchemaFilters`; `TestExtractBadJSONContinues` (um chunk inválido, outro válido: grafo tem o válido e o erro é retornado).
- [ ] **Step 2: Run** `go test ./kag -run Extract` — Expected: FAIL.
- [ ] **Step 3: Implement** `Extractor`. Concorrência não necessária (ponytail: sequencial, paralelizar com `errgroup`-like se o custo pesar).
- [ ] **Step 4: Run** `go test -race ./kag` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(kag): LLM triple extractor`.

### Task 7: KAG: planejador, solver e verificação

**Files:**
- Create: `kag/engine.go`
- Test: `kag/engine_test.go`

**Interfaces:**
- Consumes: `GraphStore`, `embeddings.Embedder`, `models.ChatModel`, `models.Structured`, `rag.Pipeline`/`knowledge.Answerer` (busca vetorial opcional via `Chunks`), `knowledge.*`.
- Produces:
  - `type Plan struct { Steps []PlanStep }`; `type PlanStep struct { ID string; Goal string; Entity string; Relations []string; Dir string; DependsOn string }` (Dir `out|in|both`; `Entity` pode ser `"$<id-do-passo>"` para encadear)
  - `type Engine struct { Model models.ChatModel; Graph GraphStore; Embedder embeddings.Embedder; Chunks knowledge.Answerer; MaxSteps int; MaxHops int; Verify bool; System string }` implementa `knowledge.Answerer` (`Strategy:"kag"`).
  - Fluxo: (1) planejar via `models.Structured[Plan]` (limite `MaxSteps`, padrão 4); (2) para cada passo, `FindEntities` (entity linking) ou entidades do passo referenciado, `Neighbors` filtrando por `Relations`/`Dir`; se vazio e `Chunks!=nil`, completar com `Chunks.Answer` e anexar as fontes; (3) acumular triplas como evidência, `Trace` com um `Step` por passo (`Name:"step:<id>"`, `Detail` com triplas); (4) síntese com evidência numerada; (5) se `Verify`, segunda chamada `models.Structured[Verdict]` (`{supported bool, unsupported []string}`); não suportado → `Trace` registra `unverified` e a resposta recebe o aviso `(não verificado)` em `Answer.Text`.
  - Travessia com `visited` por `(entidade,passo)`; `MaxHops` padrão 3 limita expansão; ciclo não repete triplas.
  - `Answer.Sources`: uma `Source` por tripla (`ID`=`From|Type|To`, `Content` = "A —TIPO→ B", `Metadata{"source": Relation.Source}`).

- [ ] **Step 1: Write failing tests** com grafo de 3 saltos (`Dr.Silva —PRESCREVEU→ X —INTERAGE_COM→ Y —EM→ Paciente`) e modelo fake `Func` que devolve plano, depois síntese: `TestMultiHopAnswers` (resposta usa evidência dos 3 saltos, `Strategy=="kag"`, `Trace` tem 3 passos, `Sources` com as 3 triplas); `TestCycleNoRepeatedTriples`; `TestFallsBackToChunks` (grafo vazio + `Chunks` fake → fontes vindas dos chunks); `TestVerifyFlagsUnsupported`; `TestMaxStepsCaps` (plano com 10 passos executa 4); `TestEmptyQuestion`; `TestBadPlanJSON` (erro claro).
- [ ] **Step 2: Run** `go test ./kag -run Engine` — Expected: FAIL.
- [ ] **Step 3: Implement** `Engine`.
- [ ] **Step 4: Run** `go test -race ./kag` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(kag): multi-hop planner, solver and verifier`.

### Task 8: Roteador

**Files:**
- Create: `knowledge/router.go`
- Test: `knowledge/router_test.go`

**Interfaces:**
- Consumes: `knowledge.Answerer`.
- Produces:
  - `type Profile struct { CorpusTokens, ContextWindow int; Volatile bool; HasGraph bool }`
  - `type Classifier interface { MultiHop(ctx context.Context, q Question) (bool, error) }`; `type LLMClassifier struct{ Model models.ChatModel }` (pergunta sim/não ao modelo); `type KeywordClassifier struct{}` (heurística pt/en: "quem", "relaciona", "entre", "who", "related", "caminho", "dependência"…; padrão quando `Classifier==nil`).
  - `type Router struct { RAG, CAG, KAG Answerer; Profile Profile; Classifier Classifier; Fallback bool }` implementa `Answerer` e `func (r *Router) Route(ctx, q Question) (strategy, reason string, err error)`.
  - Regras em ordem: `q.Force` válido e disponível vence; multi-hop && `HasGraph` && `KAG!=nil` → kag; `!Volatile` && `CorpusTokens>0` && `CorpusTokens<=ContextWindow*0.8` && `CAG!=nil` → cag; senão rag; se a escolhida for nil, usa a próxima disponível; nenhuma → `ErrNoStrategy`. `Fallback:true` repete com RAG quando a técnica escolhida retorna erro (registrado no `Trace`).
  - `Answer.Trace` ganha como primeiro passo `route: <estratégia> — <motivo>`.

- [ ] **Step 1: Write failing tests** tabelados: pergunta multi-hop com grafo → kag; corpus pequeno estável → cag; corpus grande → rag; volátil → rag; `Force:"kag"` sem KAG → erro `ErrNoStrategy`; `Force` inválido → erro; `Fallback` quando KAG falha; primeiro `Trace` é `route`.
- [ ] **Step 2: Run** `go test ./knowledge -run Router` — Expected: FAIL.
- [ ] **Step 3: Implement** o roteador.
- [ ] **Step 4: Run** `go test -race ./knowledge` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(knowledge): strategy router`.

### Task 9: Exemplos offline (rag, cag, kag, router)

**Files:**
- Create: `examples/knowledge/rag/main.go`, `examples/knowledge/cag/main.go`, `examples/knowledge/kag/main.go`, `examples/knowledge/router/main.go`
- Test: `examples/knowledge/examples_test.go` (executa `go run` de cada um via `os/exec` e confere um trecho da saída)

**Interfaces:**
- Consumes: `rag`, `cag`, `kag`, `knowledge`, `examples/internal/demo` (`demo.Smart`, `demo.Model`, `demo.Title`), `embeddings.Fake`.
- Cada `main` usa dados embutidos (constantes) e modelo fake via `demo.Smart`; imprime pergunta, resposta, fontes e `Trace`.
  - `rag`: indexa 5 documentos, mostra busca híbrida e citações `[n]`, roda o `Index` duas vezes e imprime `Skipped`.
  - `cag`: monta corpus, mostra prefixo idêntico e hash; segunda pergunta imprime `cache_hit` com fake que informa `CacheReadTokens`; mostra fallback quando `Window` é pequena.
  - `kag`: grafo de 3 saltos; mostra que `retrievers.FromVectorStore` sozinho não responde e o `kag.Engine` sim.
  - `router`: três perguntas, cada uma cai numa técnica diferente.

- [ ] **Step 1: Write failing test** `TestExamplesRun` (tabela exemplo → substring esperada: `rag`→`Skipped`, `cag`→`cache_hit`, `kag`→`PRESCREVEU`, `router`→`route: kag`).
- [ ] **Step 2: Run** `go test ./examples/knowledge` — Expected: FAIL.
- [ ] **Step 3: Implement** os quatro `main`.
- [ ] **Step 4: Run** `go test ./examples/knowledge && go run ./examples/knowledge/kag` — Expected: PASS e saída com a cadeia de triplas.
- [ ] **Step 5: Commit** `feat(examples): offline RAG, CAG, KAG and router examples`.

### Task 10: Manual e índices (fase 1)

**Files:**
- Create: `docs/manual/20-rag-cag-kag.md`, `docs/KNOWLEDGE.md`
- Modify: `README.md` (mapa de pacotes ~l.78-110, contagem de exemplos ~l.765), `docs/ARCHITECTURE.md`, `docs/manual/README.md`, `examples/README.md` (nova seção `knowledge`), `docs/manual/06-rag.md` (link para o capítulo 20), `docs/INTEGRATIONS.md` (menção)

**Interfaces:** nenhuma de código.

- [ ] **Step 1: Escrever** `docs/manual/20-rag-cag-kag.md` em pt-BR com: o que cada técnica é, matriz de decisão (do `rag_cag_kag.md`), receita mínima de cada uma (código curto e compilável copiado dos exemplos), roteador, métricas de cache (`CacheReadTokens`), limites conhecidos (heurística de tokens, indexação incremental só em processo, grafo em memória), solução de problemas. `docs/KNOWLEDGE.md`: referência dos pacotes, no estilo de `docs/CIAM.md`.
- [ ] **Step 2: Atualizar** `README.md`, `ARCHITECTURE.md`, `manual/README.md`, `examples/README.md` com pacotes novos (`knowledge`, `rag`, `cag`, `kag`) e a contagem de exemplos (+4, conferir o total atual por `find examples -name main.go | wc -l`).
- [ ] **Step 3: Verify** `grep -rn "20-rag-cag-kag" README.md docs examples` mostra os links; `go build ./... && go vet ./... && go test -race -count=1 ./...` — Expected: PASS.
- [ ] **Step 4: Commit** `docs: manual and indexes for RAG, CAG and KAG`.

---

# Fase 2: integrações

### Task 11: `knowledge.Toolkit`, MCP e nó de grafo

**Files:**
- Create: `knowledge/tools.go`, `knowledge/node.go`
- Create: `examples/knowledge/agent_tools/main.go`, `examples/knowledge/mcp_server/main.go`
- Test: `knowledge/tools_test.go`, `knowledge/node_test.go`

**Interfaces:**
- Consumes: `tools.New[T]`, `tools.ContentAndArtifact`, `server.New`, `server.CallHook`, `graph.NodeFunc` (`func(ctx, graph.State) (any, error)` em `langgraph/graph/graph.go:54`), `rag.Indexer`, `kag.Extractor`.
- Produces:
  - `type Toolkit struct { RAG, CAG, KAG Answerer; Router *Router; Ingest func(ctx context.Context, docs []documents.Document) error }`; `func (k Toolkit) All() []tools.Tool`: `rag_answer`, `cag_answer`, `kag_query`, `knowledge_answer` (via `Router`) e `knowledge_ingest` (só se `Ingest!=nil`); só inclui tools cujo `Answerer` não é nil. Argumentos: `{question string; force string(optional)}`; ingest `{documents []any}` (lista como `[]any`, cada item `{id?, content, metadata?}`). Retorno `tools.ContentAndArtifact{Content: answer.Text + fontes, Artifact: Answer}`.
  - `func (k Toolkit) MCPServer(onCall server.CallHook) *server.Server` (nome `"knowledge"`, mesma forma de `ciam.Toolkit.MCPServer`).
  - `func Node(a Answerer, questionKey, answerKey string) graph.NodeFunc`: lê `state[questionKey]` (string), devolve `graph.State{answerKey: ans.Text, answerKey+"_sources": ans.Sources}`.
  - Exemplos: `agent_tools` (agente de `agents.Create` usando as tools com modelo fake que chama `rag_answer`); `mcp_server` (sobe `MCPServer` em stdio, imprime instrução de uso quando executado sem stdin interativo).

- [ ] **Step 1: Write failing tests**: `TestToolkitListsOnlyAvailable`; `TestToolArgsAreAnySlice` (ingest com `[]any`); `TestToolReturnsArtifact`; `TestMCPServerToolsList` e `TestMCPCall` (via `server.Handle` com JSON-RPC `tools/list` e `tools/call`, como em `mcp/server/server_test.go`); `TestNode`.
- [ ] **Step 2: Run** `go test ./knowledge -run 'Toolkit|MCP|Node'` — Expected: FAIL.
- [ ] **Step 3: Implement** `Toolkit`, `MCPServer`, `Node` e os dois exemplos (offline).
- [ ] **Step 4: Run** `go test -race ./knowledge ./examples/knowledge && go run ./examples/knowledge/agent_tools` — Expected: PASS; saída com resposta citando fonte.
- [ ] **Step 5: Commit** `feat(knowledge): agent tools, MCP server and graph node`.

### Task 12: Adapter de grafo Neo4j

**Files:**
- Create: `integrations/graphstore/neo4j.go`, `integrations/graphstore/cypher.go` (queries e validação de identificadores, compartilhado com Memgraph)
- Test: `integrations/graphstore/neo4j_test.go`

**Interfaces:**
- Consumes: `kag.GraphStore`; mesmo protocolo HTTP de `integrations/vectorstore/neo4j.go` (`POST /db/<db>/query/v2`, basic auth, `{"statement","parameters"}`, resposta `data.values`).
- Produces: `type Neo4j struct { URL, Username, Password, Database string; Label string; HTTP *http.Client }` (`Label` padrão `"Entity"`); implementa `kag.GraphStore`. Entidade = nó `:Entity {id,name,type,props,vector}`; relação = aresta com **tipo dinâmico**, que não pode ser parâmetro em Cypher: validar com `^[A-Z][A-Z0-9_]*$` antes de interpolar e rejeitar o resto com erro (nunca concatenar cru). `Path` usa `shortestPath`/`allShortestPaths` com limite `*1..maxHops` (inteiro validado). `FindEntities` por nome (`toLower(n.name) CONTAINS $t`) e por vetor via `vector.similarity.cosine` quando `vec!=nil`.
  - `props` serializado como JSON string.

- [ ] **Step 1: Write failing tests** com `httptest.Server` que captura `statement` e `parameters`: `TestUpsertUsesParameters` (valores nunca no texto do statement), `TestRelationTypeValidated` (`"X`) DETACH DELETE n //"` → erro, nenhuma requisição enviada), `TestNeighborsParsesRows`, `TestHTTPErrorSurfaces`; mais `TestNeo4jReal` que pula sem `NEO4J_URL` e roda `conformance.Graph(t, &Neo4j{...})` quando definido.
- [ ] **Step 2: Run** `go test ./integrations/graphstore` — Expected: FAIL.
- [ ] **Step 3: Implement** `Neo4j` e `cypher.go` (`validIdent`, `buildPathQuery`).
- [ ] **Step 4: Run** `go test -race ./integrations/graphstore` — Expected: PASS (real pulado).
- [ ] **Step 5: Commit** `feat(graphstore): Neo4j GraphStore`.

### Task 13: Adapter de grafo Memgraph

**Files:**
- Create: `integrations/graphstore/memgraph.go`
- Test: `integrations/graphstore/memgraph_test.go`

**Interfaces:**
- Consumes: `memgraph.Client.Run(ctx, query, params) ([]string, [][]any, error)`; `cypher.go` (Task 12); `kag.GraphStore`.
- Produces: `type Memgraph struct { Client *memgraph.Client; Label string }` implementa `kag.GraphStore` com as mesmas consultas Cypher do Neo4j, adaptando o que difere: caminhos por `[*BFS ..maxHops]` ou `shortestPath` conforme suporte do Memgraph (conferir a doc antes) e similaridade vetorial por `vector_search.search` quando houver índice, senão cálculo em Go sobre os candidatos. Valores de `[][]any` chegam como `int64`, `float64`, `string`, `map[string]any`, `[]any` (ver `unpack` em `integrations/memgraph/bolt.go`).

- [ ] **Step 1: Write failing tests**: um servidor Bolt falso é trabalhoso demais; testar a camada de conversão (`rowsToEntities`, `rowsToRelations`) com linhas sintéticas e a validação de identificadores; `TestMemgraphReal` pula sem `MEMGRAPH_ADDR` e roda `conformance.Graph` quando definido.
- [ ] **Step 2: Run** `go test ./integrations/graphstore -run Memgraph` — Expected: FAIL.
- [ ] **Step 3: Implement** `Memgraph`.
- [ ] **Step 4: Run** `go test -race ./integrations/graphstore` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(graphstore): Memgraph GraphStore`.

### Task 14: Suíte de conformidade do RAG sobre backends existentes

**Files:**
- Create: `internal/conformance/rag.go`
- Test: `rag/conformance_test.go`

**Interfaces:**
- Produces: `conformance.RAG(t *testing.T, newStore func() vectorstores.VectorStore)`: indexa corpus pequeno, confere recuperação, filtro de metadados, remoção por reindexação e resposta com fontes.
- Consumes: `vectorstores.NewMemory`, `vectorstore.New(emb, &vectorstore.Local{...})` (conferir construtor em `integrations/vectorstore/local.go`) e um backend `httptest` já usado em `integrations/vectorstore/remote_test.go`.

- [ ] **Step 1: Write failing test** `TestRAGConformance` rodando a suíte contra `Memory`, `Local` e o backend `httptest`.
- [ ] **Step 2: Run** `go test ./rag -run Conformance` — Expected: FAIL.
- [ ] **Step 3: Implement** `conformance.RAG`.
- [ ] **Step 4: Run** `go test -race ./rag ./internal/conformance` — Expected: PASS.
- [ ] **Step 5: Commit** `test(rag): conformance suite over existing vector backends`.

### Task 15: CLI `cmd/knowledge`

**Files:**
- Create: `cmd/knowledge/main.go`
- Test: `cmd/knowledge/main_test.go`

**Interfaces:**
- Consumes: `rag`, `cag`, `kag`, `knowledge`, `vectorstores.Memory.Save/Load`, `kag.Memory.Save/LoadMemory`, `chatmodels.Init`, `embeddings.Fake` (padrão offline), `providers/ollama` ou `chatmodels` para embeddings reais quando configurado, `knowledge.Toolkit`.
- Produces (padrão de `cmd/ciam/main.go`, texto de ajuda em constante `usage`):
  - `knowledge index <path> [--glob "*.md"] [--kind rag|kag|both] [--out dir]`: indexa e salva `vectors.json` e `graph.json` em `--out` (padrão `./.knowledge`).
  - `knowledge ask "pergunta" [--mode auto|rag|cag|kag] [--dir ./.knowledge] [--src path]`: roteia e imprime resposta, fontes e `Trace`; `cag` usa `--src` como corpus.
  - `knowledge mcp [--dir]`: servidor MCP em stdio; `knowledge serve [--addr :8080]`: MCP sobre HTTP.
  - Modelo: `KNOWLEDGE_MODEL` (padrão `anthropic:claude-sonnet-5-5` se houver chave, senão fake com aviso). Embeddings: `embeddings.Fake` salvo `KNOWLEDGE_EMBEDDINGS=ollama:<model>`.
  - Função testável `run(args []string, stdout io.Writer) error`.

- [ ] **Step 1: Write failing tests** em diretório temporário: `TestIndexThenAsk` (indexa 2 `.md`, `ask --mode rag` imprime fonte do arquivo certo), `TestAskKagNeedsGraph` (erro claro sem `graph.json`), `TestUsageOnNoArgs`, `TestUnknownMode`.
- [ ] **Step 2: Run** `go test ./cmd/knowledge` — Expected: FAIL.
- [ ] **Step 3: Implement** `run` e os subcomandos.
- [ ] **Step 4: Run** `go test -race ./cmd/knowledge && go run ./cmd/knowledge` — Expected: PASS; sem argumentos imprime o `usage`.
- [ ] **Step 5: Commit** `feat(cmd): knowledge CLI`.

### Task 16: Documentação das integrações e verificação final

**Files:**
- Modify: `docs/manual/20-rag-cag-kag.md` (seções Tools, MCP, adapters de grafo, CLI, combinações recomendadas vetor × grafo), `docs/KNOWLEDGE.md`, `docs/INTEGRATIONS.md` (Neo4j e Memgraph como GraphStore; env `NEO4J_URL`, `MEMGRAPH_ADDR`), `README.md`, `docs/ARCHITECTURE.md`, `docs/manual/README.md`, `examples/README.md` (contagens: +2 exemplos), `CLAUDE.md` (linha de arquitetura para `knowledge/rag/cag/kag`)

- [ ] **Step 1: Atualizar** os documentos acima em pt-BR; as contagens de pacotes e exemplos devem bater com `find`.
- [ ] **Step 2: Verify** `go build ./... && go vet ./... && go test -race -count=1 ./...` — Expected: PASS (testes de serviço real pulados).
- [ ] **Step 3: Verify** `grep -rn "gopkg\|github.com/" --include=*.go rag cag kag knowledge integrations/graphstore | grep -v golang-chain-eco` não mostra import de terceiros.
- [ ] **Step 4: Commit** `docs: integrations for RAG, CAG and KAG`.

---

## Self-Review

- **Cobertura do design:** contrato comum (T2), RAG (T3), CAG com métricas de cache e contador heurístico (T1, T2, T4), KAG com grafo, extração, planejamento, verificação (T5-T7), roteador (T8), exemplos (T9, T11), manual e índices (T10, T16), Toolkit/MCP/nó de grafo (T11), adapters Neo4j e Memgraph (T12, T13), conformidade sobre backends existentes (T14), CLI (T15). Ordem pedida: núcleo e exemplos antes das integrações.
- **Consistência de tipos:** `knowledge.Question/Answer/Source/Step`, `kag.GraphStore`, `kag.Entity/Relation`, `cag.PrefixCacher`, `rag.Pipeline` aparecem com a mesma assinatura em todas as tasks que os consomem.
- **Lacunas assumidas (decisão do plano):** indexação incremental só em processo; `knowledge` não importa `rag/cag/kag` (evita ciclo); o CLI reindexa a partir do diretório quando o estado de hashes não existe.
