# ciam: contexto cirúrgico para assistentes de IA (`ciam/`)

Port, em Go e integrado a este ecossistema, das ideias do [Context IA Manager](https://github.com/smkbarbosa/context-ia-manager): em vez de mandar arquivos inteiros para o modelo, **indexe o projeto**, **busque só o que importa** (semântica + palavras-chave), **guarde o porquê** (ADR, PRD, planos) e **lembre decisões** entre sessões. Tudo exposto como `tools.Tool` (para agents deste projeto) e como **servidor MCP** (VS Code, Cursor, Claude Code...).

Tutorial: [manual/18-ciam-contexto.md](manual/18-ciam-contexto.md). Exemplos: [`examples/ciam/`](../examples/ciam/). Servidor MCP genérico: pacote [`mcp/server`](../mcp/server).

> **Sobre a origem.** O repositório original não traz arquivo de licença; por isso este port é uma reimplementação a partir do comportamento descrito no README e da leitura do código, **sem copiar texto**: templates, prompts e tabelas são originais. Os nomes das ferramentas (`ciam_*`) foram mantidos para compatibilidade de uso.

## Mapa original → Go

| Original (ciam) | Aqui |
|---|---|
| `ciam index` (SQLite, hash por arquivo) | `Manager.Index` sobre qualquer `store.Store`; incremental por SHA-256 |
| busca híbrida (vetor + BM25 simplificado, RRF) | `Manager.Search`: cosseno + **BM25 com IDF real do corpus** + RRF (k=60) |
| tipos de chunk Django (`model`, `view`, ...) | `Classifier` plugável: Django, Go, JS/TS/Python, genérico (`test`, `handler`, `config`, `doc`, `schema`) |
| `ciam_compress` | `Compress` (Python `#`/docstrings; Go/JS `//` e `/* */`) |
| `ciam_remember` / `ciam_recall` | `Remember`/`Recall`, memórias do **langmem** (`IndexedStore`) no namespace `["ciam","memories"]` |
| `adr`/`prd`/`plan` `new`/`list`/`supersede` | `Docs.New/List/SupersedeADR/SetStatus` (+ `research`) |
| `ciam_adr/prd/plan/research_search` | tools com filtro por tipo de chunk |
| `ciam_decision_context` | `Manager.DecisionContext`: código + ADR + PRD + planos + pesquisa |
| `ciam_django_map` | `ProjectMap` (Django: apps; Go: pacotes; demais: diretórios) — tool `ciam_project_map` |
| `ciam_route` | `Route` (tabela de intenções → ferramenta) |
| `ciam_draft` (Ollama local) | `Manager.Draft` com qualquer `models.ChatModel` (`Manager.Model`) |
| cache L1/L2, métricas | cache em memória (LRU por idade + TTL) e `Status` (tokens servidos / economizados estimados) |
| servidor MCP (mcp-go) | `Toolkit.MCPServer()` sobre `mcp/server` (stdio e HTTP) |
| `ciam watch` | `Manager.Watch` (polling incremental; sem fsnotify, só stdlib) |
| `ciam init` (6 editores) | `ciam init --editor vscode\|cursor\|claude` (ou `--print`) |

## API

```go
m := ciam.New(embedder, store)                 // store nil = memória; ciam.OpenFileStore(path) persiste em JSON
res, _ := m.Index(ctx, "/proj", ciam.IndexOptions{})  // incremental: só reembeda o que mudou
hits, _ := m.Search(ctx, ciam.SearchRequest{Project: "proj", Query: "jwt auth", Types: []string{"view"}, Limit: 5, Compress: true})
dc, _ := m.DecisionContext(ctx, "proj", "adicionar refresh token", 3)
m.Remember(ctx, "decision", "...") ; m.Recall(ctx, "...", 3)

docs := ciam.NewDocs("/proj")
docs.New(ciam.ADR, "Usar UUID como PK", "") ; docs.New(ciam.Plan, "Integração Stripe", "PRD-001")
docs.SupersedeADR(3, "Usar ULID como PK")      // o antigo vira "superseded": histórico não se reescreve

kit := ciam.Toolkit{M: m, Project: "/proj"}
kit.All()                                      // []tools.Tool para um agent
kit.MCPServer(nil).ServeStdio(ctx, os.Stdin, os.Stdout)
```

**Indexação.** Percorre o projeto ignorando ocultos e `node_modules`, `vendor`, `venv`, `dist`... (arquivos > 1 MB também). Janelas de 50 linhas com 5 de sobreposição, cortadas em 1800 bytes (limite de contexto de modelos de embedding pequenos); documentos em `docs/{adr,prd,plans,research}` entram com tipo próprio (`adr`, `prd`, `plan`, `research`) em janelas de 80 linhas. O hash do arquivo decide se ele é reembeddado; arquivos removidos saem do índice. O id do projeto é o nome do diretório raiz.

**Busca.** Dois rankings sobre os candidatos (após filtro de tipo): similaridade de cosseno e BM25 (identificadores `snake_case`/`camelCase` são partidos, então `stripe customer` acha `stripe_customer_id`). Fusão **RRF** (`Σ 1/(60+posição)`); trecho sem nenhuma palavra em comum não ganha posição no ranking de palavras. `Compress` remove docstrings e comentários antes de devolver. Resultados repetidos vêm do cache (padrão 256 entradas, 10 min), invalidado ao reindexar.

**Embedder.** Qualquer `embeddings.Embedder`: `providers/ollama.Embeddings{Model: "nomic-embed-text"}` para 100% local. A CLI testa o Ollama e, se não responde, usa o embedder por hashing (`CIAM_EMBEDDER=fake` força) — a busca então depende mais do BM25.

**Armazenamento.** `store.Store`: chunks em `["ciam","chunks",projeto]`, hashes em `["ciam","files",projeto]`, memórias em `["ciam","memories"]`. `FileStore` guarda tudo em um JSON (escrita atômica) — bom para um dev; para times, projetos grandes ou vários processos use `sqlstore`, Redis ou Mongo. A busca carrega o corpus do projeto em RAM (invalidado ao reindexar), então o custo por busca é O(chunks).

**Documentos.** ADR (`status`, contexto, decisão, consequências, alternativas), PRD (problema, objetivo, escopo, requisitos, critérios de aceite), Plan (fases com critérios automatizáveis e verificação manual, pausa entre fases, `prd:` referenciando o PRD), Research. Arquivos `docs/<tipo>/<PREFIXO>-NNN-slug.md` com *front matter* YAML (`status`, `date`, `title`); numeração sequencial. `Docs.PlanExcerpt(n, "Phase 2")` extrai uma fase para prompts.

**`ciam_draft`.** Só existe se `Manager.Model` estiver configurado (CLI: `CIAM_CODE_MODEL`, via Ollama). Monta o prompt com o contexto de decisão + o trecho do plano e devolve um rascunho **rotulado como especulativo** — é para economizar tokens pagos, não para aceitar sem revisão.

## CLI

```bash
go install ./cmd/ciam
ciam adr new "Usar JWT para sessões"     # escreva o porquê ANTES do código
ciam index                               # incremental
ciam search "autenticação" --type view --limit 3
ciam decision "adicionar refresh token"  # código + ADR + PRD + planos
ciam remember "decidimos usar UUID como PK" --type decision ; ciam recall "chaves primárias"
ciam mcp                                 # servidor MCP em stdio
ciam init --editor claude                # grava .mcp.json (mescla com servidores existentes)
CIAM_API_KEY=segredo ciam api --addr :8080   # API JSON: POST /tools/{name}, GET /tools, GET /status, GET /healthz
```

`ciam api` expõe as mesmas tools em JSON puro, sem MCP: `POST /tools/ciam_search` com o corpo `{"query": "..."}` devolve o resultado; nome desconhecido responde 404, argumentos inválidos 400. Com `CIAM_API_KEY` definida, todas as rotas (exceto `/healthz`) exigem o header `X-Api-Key`, senão 401. Em código: `ciam.Toolkit{M: m, Project: root}.Handler(apiKey)`.

Variáveis: `CIAM_DB_PATH` (padrão `~/.local/share/ciam/ciam.json`), `CIAM_PROJECT_PATH`, `CIAM_OLLAMA_URL`/`OLLAMA_HOST`, `CIAM_OLLAMA_MODEL`, `CIAM_CODE_MODEL`, `CIAM_EMBEDDER`, `CIAM_API_KEY`.

## Integração com os agents

- `openswe.Config{Context: &ciam.Toolkit{...}}` dá ao engenheiro as tools e a instrução "consulte antes de ler arquivos e antes de implementar" (`ciam.Instructions`).
- Em qualquer `agents.Create`/`deepagents.Create`: `Tools: kit.All()` e `SystemPrompt: ciam.Instructions`.
- Servir para editores: `Toolkit.MCPServer(onCall)`; `onCall` recebe nome, argumentos, duração e erro de cada chamada (métricas).

## Diferenças em relação ao original (sem maquiagem)

- **Sem cache L2 persistente** de buscas (só em memória) e sem sqlite-vec: o índice vive no `store.Store`. O cosseno é por varredura por padrão; com `Manager.IndexFactory` (ex.: `func() langmem.VectorIndex { return vectorstore.NewHNSW() }`) ele usa um HNSW por projeto, reconstruído quando o índice do projeto muda. O BM25 continua exato.
- **Métricas** são contadores do processo (`Status`), não tabela persistente por ferramenta.
- **Edição genérica**: o original é focado em Django; aqui os tipos Django continuam, e há regras para Go/JS/Python/config, mas sem análise sintática (classificação por nome de arquivo e dicas de conteúdo).
- Templates de ADR/PRD/Plan são **originais e em inglês** (o original usa português); edite `ciam/templates.go` para o idioma do seu time.
- `watch` é polling (padrão 30 s), não eventos do sistema de arquivos.
- `ciam init` cobre VS Code, Cursor e Claude Code; para Windsurf, Cline, OpenCode, Antigravity use `--print` e cole o JSON.

## Testes

`go test -race ./ciam/`: classificação e indexação incremental (alterado/removido), busca híbrida (identificadores, filtros, compressão, cache), ciclo de ADR/PRD/Plan (supersede, status, vínculo plano→PRD, trecho de fase), `DecisionContext` sem vazamento de documentos na seção de código, persistência em `FileStore` (reabrir → mesma busca, mesmos hashes; arquivo corrompido = erro), memória, `Toolkit` ponta a ponta por MCP (cliente `mcp` real sobre pipes), `Draft`, `Compress`/`Tokenize` e `ProjectMap`.
