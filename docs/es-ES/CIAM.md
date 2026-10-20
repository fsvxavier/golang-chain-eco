# ciam: contexto quirúrgico para asistentes de IA (`ciam/`)

[English](../en-US/CIAM.md) | [Português](../pt-BR/CIAM.md) | **Español**

Port, en Go e integrado a este ecosistema, de las ideas de [Context IA Manager](https://github.com/smkbarbosa/context-ia-manager): en lugar de mandar archivos enteros al modelo, **indexa el proyecto**, **busca solo lo que importa** (semántica + palabras clave), **guarda el porqué** (ADR, PRD, planes) y **recuerda decisiones** entre sesiones. Todo expuesto como `tools.Tool` (para agents de este proyecto) y como **servidor MCP** (VS Code, Cursor, Claude Code...).

Tutorial: [manual/18-ciam-contexto.md](manual/18-ciam-contexto.md). Ejemplos: [`examples/ciam/`](../../examples/ciam/). Servidor MCP genérico: paquete [`mcp/server`](../../mcp/server).

> **Sobre el origen.** El repositorio original no trae archivo de licencia; por eso este port es una reimplementación a partir del comportamiento descrito en el README y de la lectura del código, **sin copiar texto**: plantillas, prompts y tablas son originales. Los nombres de las herramientas (`ciam_*`) se mantuvieron por compatibilidad de uso.

## Mapa original → Go

| Original (ciam) | Aquí |
|---|---|
| `ciam index` (SQLite, hash por archivo) | `Manager.Index` sobre cualquier `store.Store`; incremental por SHA-256 |
| búsqueda híbrida (vector + BM25 simplificado, RRF) | `Manager.Search`: coseno + **BM25 con IDF real del corpus** + RRF (k=60) |
| tipos de chunk de Django (`model`, `view`, ...) | `Classifier` enchufable: Django, Go, JS/TS/Python, genérico (`test`, `handler`, `config`, `doc`, `schema`) |
| `ciam_compress` | `Compress` (Python `#`/docstrings; Go/JS `//` y `/* */`) |
| `ciam_remember` / `ciam_recall` | `Remember`/`Recall`, memorias de **langmem** (`IndexedStore`) en el namespace `["ciam","memories"]` |
| `adr`/`prd`/`plan` `new`/`list`/`supersede` | `Docs.New/List/SupersedeADR/SetStatus` (+ `research`) |
| `ciam_adr/prd/plan/research_search` | herramientas con filtro por tipo de chunk |
| `ciam_decision_context` | `Manager.DecisionContext`: código + ADR + PRD + planes + investigación |
| `ciam_django_map` | `ProjectMap` (Django: apps; Go: paquetes; demás: directorios) — herramienta `ciam_project_map` |
| `ciam_route` | `Route` (tabla de intenciones → herramienta) |
| `ciam_draft` (Ollama local) | `Manager.Draft` con cualquier `models.ChatModel` (`Manager.Model`) |
| caché L1/L2, métricas | caché en memoria (LRU por antigüedad + TTL) y `Status` (tokens servidos / ahorrados estimados) |
| servidor MCP (mcp-go) | `Toolkit.MCPServer()` sobre `mcp/server` (stdio y HTTP) |
| `ciam watch` | `Manager.Watch` (polling incremental; sin fsnotify, solo stdlib) |
| `ciam init` (6 editores) | `ciam init --editor vscode\|cursor\|claude` (o `--print`) |

## API

```go
m := ciam.New(embedder, store)                 // store nil = memoria; ciam.OpenFileStore(path) persiste en JSON
res, _ := m.Index(ctx, "/proj", ciam.IndexOptions{})  // incremental: solo reembebe lo que cambió
hits, _ := m.Search(ctx, ciam.SearchRequest{Project: "proj", Query: "jwt auth", Types: []string{"view"}, Limit: 5, Compress: true})
dc, _ := m.DecisionContext(ctx, "proj", "agregar refresh token", 3)
m.Remember(ctx, "decision", "...") ; m.Recall(ctx, "...", 3)

docs := ciam.NewDocs("/proj")
docs.New(ciam.ADR, "Usar UUID como PK", "") ; docs.New(ciam.Plan, "Integración Stripe", "PRD-001")
docs.SupersedeADR(3, "Usar ULID como PK")      // el antiguo pasa a "superseded": el historial no se reescribe

kit := ciam.Toolkit{M: m, Project: "/proj"}
kit.All()                                      // []tools.Tool para un agent
kit.MCPServer(nil).ServeStdio(ctx, os.Stdin, os.Stdout)
```

**Indexación.** Recorre el proyecto ignorando ocultos y `node_modules`, `vendor`, `venv`, `dist`... (archivos > 1 MB también). Ventanas de 50 líneas con 5 de solapamiento, cortadas en 1800 bytes (límite de contexto de los modelos de embedding pequeños); los documentos en `docs/{adr,prd,plans,research}` entran con tipo propio (`adr`, `prd`, `plan`, `research`) en ventanas de 80 líneas. El hash del archivo decide si se reembebe; los archivos eliminados salen del índice. El id del proyecto es el nombre del directorio raíz.

**Búsqueda.** Dos rankings sobre los candidatos (tras filtrar por tipo): similitud de coseno y BM25 (los identificadores `snake_case`/`camelCase` se parten, así que `stripe customer` encuentra `stripe_customer_id`). Fusión **RRF** (`Σ 1/(60+posición)`); un fragmento sin ninguna palabra en común no obtiene posición en el ranking de palabras. `Compress` quita docstrings y comentarios antes de devolver. Los resultados repetidos vienen de la caché (predeterminado 256 entradas, 10 min), invalidada al reindexar.

**Embedder.** Cualquier `embeddings.Embedder`: `providers/ollama.Embeddings{Model: "nomic-embed-text"}` para 100% local. La CLI prueba Ollama y, si no responde, usa el embedder por hashing (`CIAM_EMBEDDER=fake` lo fuerza) — la búsqueda entonces depende más del BM25.

**Almacenamiento.** `store.Store`: chunks en `["ciam","chunks",proyecto]`, hashes en `["ciam","files",proyecto]`, memorias en `["ciam","memories"]`. `FileStore` guarda todo en un JSON (escritura atómica) — bien para un dev; para equipos, proyectos grandes o varios procesos usa `sqlstore`, Redis o Mongo. La búsqueda carga el corpus del proyecto en RAM (invalidado al reindexar), así que el costo por búsqueda es O(chunks).

**Documentos.** ADR (`status`, contexto, decisión, consecuencias, alternativas), PRD (problema, objetivo, alcance, requisitos, criterios de aceptación), Plan (fases con criterios automatizables y verificación manual, pausa entre fases, `prd:` que referencia el PRD), Research. Archivos `docs/<tipo>/<PREFIJO>-NNN-slug.md` con *front matter* YAML (`status`, `date`, `title`); numeración secuencial. `Docs.PlanExcerpt(n, "Phase 2")` extrae una fase para los prompts.

**`ciam_draft`.** Solo existe si `Manager.Model` está configurado (CLI: `CIAM_CODE_MODEL`, vía Ollama). Arma el prompt con el contexto de decisión + el fragmento del plan y devuelve un borrador **etiquetado como especulativo** — sirve para ahorrar tokens pagos, no para aceptarlo sin revisión.

## CLI

```bash
go install ./cmd/ciam
ciam adr new "Usar JWT para sesiones"    # escribe el porqué ANTES del código
ciam index                               # incremental
ciam search "autenticación" --type view --limit 3
ciam decision "agregar refresh token"    # código + ADR + PRD + planes
ciam remember "decidimos usar UUID como PK" --type decision ; ciam recall "claves primarias"
ciam mcp                                 # servidor MCP en stdio
ciam init --editor claude                # escribe .mcp.json (se combina con los servidores existentes)
CIAM_API_KEY=secreto ciam api --addr :8080   # API JSON: POST /tools/{name}, GET /tools, GET /status, GET /healthz
```

`ciam api` expone las mismas herramientas en JSON puro, sin MCP: `POST /tools/ciam_search` con el cuerpo `{"query": "..."}` devuelve el resultado; un nombre desconocido responde 404, argumentos inválidos 400. Con `CIAM_API_KEY` definida, todas las rutas (excepto `/healthz`) exigen el header `X-Api-Key`, si no 401. En código: `ciam.Toolkit{M: m, Project: root}.Handler(apiKey)`.

Variables: `CIAM_DB_PATH` (predeterminado `~/.local/share/ciam/ciam.json`), `CIAM_PROJECT_PATH`, `CIAM_OLLAMA_URL`/`OLLAMA_HOST`, `CIAM_OLLAMA_MODEL`, `CIAM_CODE_MODEL`, `CIAM_EMBEDDER`, `CIAM_API_KEY`.

## Integración con los agents

- `openswe.Config{Context: &ciam.Toolkit{...}}` le da al ingeniero las herramientas y la instrucción "consulta antes de leer archivos y antes de implementar" (`ciam.Instructions`).
- En cualquier `agents.Create`/`deepagents.Create`: `Tools: kit.All()` y `SystemPrompt: ciam.Instructions`.
- Para servir a editores: `Toolkit.MCPServer(onCall)`; `onCall` recibe nombre, argumentos, duración y error de cada llamada (métricas).

## Diferencias respecto al original (sin maquillaje)

- **Sin caché L2 persistente** de búsquedas (solo en memoria) y sin sqlite-vec: el índice vive en el `store.Store`. El coseno es por escaneo de forma predeterminada; con `Manager.IndexFactory` (ej.: `func() langmem.VectorIndex { return vectorstore.NewHNSW() }`) usa un HNSW por proyecto, reconstruido cuando cambia el índice del proyecto. El BM25 sigue siendo exacto.
- **Métricas** son contadores del proceso (`Status`), no una tabla persistente por herramienta.
- **Edición genérica**: el original está centrado en Django; aquí se mantienen los tipos de Django y hay reglas para Go/JS/Python/config, pero sin análisis sintáctico (clasificación por nombre de archivo y pistas de contenido).
- Las plantillas de ADR/PRD/Plan son **originales y en inglés** (el original usa portugués); edita `ciam/templates.go` para el idioma de tu equipo.
- `watch` es polling (predeterminado 30 s), no eventos del sistema de archivos.
- `ciam init` cubre VS Code, Cursor y Claude Code; para Windsurf, Cline, OpenCode, Antigravity usa `--print` y pega el JSON.

## Pruebas

`go test -race ./ciam/`: clasificación e indexación incremental (modificado/eliminado), búsqueda híbrida (identificadores, filtros, compresión, caché), ciclo de ADR/PRD/Plan (supersede, estado, vínculo plan→PRD, fragmento de fase), `DecisionContext` sin fuga de documentos en la sección de código, persistencia en `FileStore` (reabrir → misma búsqueda, mismos hashes; archivo corrupto = error), memoria, `Toolkit` de extremo a extremo por MCP (cliente `mcp` real sobre pipes), `Draft`, `Compress`/`Tokenize` y `ProjectMap`.
