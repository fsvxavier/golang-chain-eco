# 18. ciam: contexto de proyecto barato y preciso para asistentes

[English](../../en-US/manual/18-ciam-contexto.md) | [Português](../../pt-BR/manual/18-ciam-contexto.md) | **Español**

Los asistentes de IA gastan tokens releyendo archivos enteros y aun así se equivocan porque no conocen el **porqué** del código. El paquete `ciam` resuelve los dos lados: un **índice local con búsqueda híbrida** (devuelve solo los fragmentos relevantes) y una carpeta `docs/` con **ADR, PRD y planes** versionados y consultables. Referencia y límites: [docs/es-ES/CIAM.md](../CIAM.md).

Requisitos previos: capítulo 6 (embeddings y RAG) y, para la parte de agents, capítulos 5 y 9.

## 18.1 El flujo en una pantalla

```bash
go install ./cmd/ciam
cd mi-proyecto
ciam adr new "Usar JWT para sesiones"    # 1. escribe el PORQUÉ antes del código
ciam index                               # 2. indexa (incremental)
ciam search "autenticación" --type view  # 3. busca fragmentos, no archivos
ciam decision "agregar refresh token"    # 4. código + ADR + PRD + planes en una llamada
ciam remember "los tokens expiran en 15 min" --type decision
ciam init --editor claude                # 5. conecta el editor (MCP)
```

Sin Ollama en ejecución, la CLI usa embeddings por hashing y lo avisa; para búsqueda semántica real: `ollama pull nomic-embed-text` (100% local).

## 18.2 Indexar y buscar desde la API

```go
emb := &ollama.Embeddings{Model: "nomic-embed-text"}
fs, _ := ciam.OpenFileStore("ciam.json")        // o cualquier store.Store (sqlstore, redis, mongo)
m := ciam.New(emb, fs)

res, _ := m.Index(ctx, "/proyecto", ciam.IndexOptions{})
fmt.Println(res.Changed, "archivos reprocesados,", res.Unchanged, "intactos")

hits, _ := m.Search(ctx, ciam.SearchRequest{
	Project: "proyecto", Query: "validación del token JWT",
	Types: []string{"view", "handler"}, Limit: 5, Compress: true,
})
```

- **Incremental**: solo los archivos con hash nuevo se trocean y se embeben; los eliminados salen del índice. Reindexar cuesta casi nada (`ciam watch` lo hace cada 30 s).
- **Híbrida**: `Result.Vector` (coseno) y `Result.BM25` muestran cada mitad; la nota final es la fusión por *reciprocal rank*. Los identificadores `snake_case`/`camelCase` se parten: "stripe customer" encuentra `stripe_customer_id`.
- **Tipos de chunk**: `model`, `view`, `url`, `serializer`, `task`, `test`, `handler`, `config`, `doc`, `schema`, `generic` y los documentos `adr`, `prd`, `plan`, `research`. Para otros patrones, pasa `Manager.Classifiers`.
- **Compresión**: `Compress: true` quita docstrings/comentarios; ahorra tokens en la respuesta.

`m.Status(ctx)` muestra los tokens servidos y la **estimación** de lo que habría costado leer los archivos completos.

## 18.3 ADR, PRD y planes

```go
docs := ciam.NewDocs("/proyecto")
docs.New(ciam.PRD, "Facturación con Stripe", "")
docs.New(ciam.Plan, "Integración Stripe", "PRD-001")   // vincula el plan al PRD
docs.New(ciam.ADR, "Corregir race en el worker de e-mails", "")  // causa raíz ANTES del fix
docs.SupersedeADR(3, "Usar ULID como clave primaria")  // ADR-003 pasa a "superseded"; nada se reescribe
list, _ := docs.List(ciam.ADR)                         // número, título, estado
```

Archivos en `docs/es-ES/adr|prd|plans|research/PREFIJO-NNN-slug.md` con *front matter* (`status`, `date`, `title`). Después de escribirlos, `ciam index`: se convierten en chunks consultables. Regla de oro: **funcionalidad nueva → PRD + plan; bug relevante → ADR con la causa raíz; cambiaste de idea → supersede, no edites**.

`m.DecisionContext(ctx, proyecto, "lo que voy a hacer", 3)` devuelve en Markdown: código (comprimido), ADR, PRD, planes e investigaciones relevantes — es lo que un agent debe consultar **antes de implementar**.

## 18.4 Memoria entre sesiones

`Remember`/`Recall` guardan decisiones y notas que valen más allá de un proyecto ("desplegar solo los martes"). Son memorias de `langmem` (cap. 16) en el namespace `["ciam","memories"]`: un `langmem.Manager` o la tool `manage_memory` también pueden curarlas.

## 18.5 Dar el contexto a un agent

```go
kit := ciam.Toolkit{M: m, Project: "/proyecto"}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

Tools (`ciam_*`): `route`, `index`, `search`, `context` (búsqueda + compresión), `decision_context`, `project_map`, `adr_search`, `prd_search`, `plan_search`, `research_search`, `remember`, `recall`, `compress`, `doc_new` y, si existe `Manager.Model`, `draft`. `ciam.Instructions` enseña el orden: **decision_context antes de implementar; search antes de leer archivos; registra decisiones**. Ejemplo: `go run ./examples/ciam/agent_with_context` — el agent descubre, por el ADR, que el dinero es `int64` en centavos, algo que el pedido no decía.

En el ingeniero de Open SWE (cap. 19): `openswe.Config{Context: &kit}`.

## 18.6 Servir al editor (MCP)

```bash
ciam init --editor vscode     # o cursor / claude; combina con los servidores ya configurados
```

Esto escribe `{"command": ".../ciam", "args": ["mcp"], "env": {"CIAM_PROJECT_PATH": ...}}`. En código: `kit.MCPServer(onCall).ServeStdio(ctx, os.Stdin, os.Stdout)` o `.Handler()` para HTTP (`ciam serve`). El servidor también publica *prompts* (`implement-feature`, `investigate-bug`) y las instrucciones de arriba. El paquete `mcp/server` es genérico: sirve cualquier `[]tools.Tool` tuya.

## 18.7 Borradores con un modelo local (opcional)

`Manager.Model = ollama.New("qwen2.5-coder:1.5b")` habilita `ciam_draft`: un borrador de código anclado en el contexto y en una fase del plan (`PlanID`, `Phase: "Phase 1"`), marcado como **especulativo**. Úsalo para ahorrar tokens pagos en código repetitivo; valídalo contra el plan y los ADR.

## 18.8 Cuidados

- **Secretos**: el índice contiene el código. Mantén `.env`, claves y volcados fuera del proyecto indexado (o fuera de las extensiones indexables), y guarda el archivo del índice con los mismos permisos que el código.
- **Escala**: la búsqueda carga el corpus del proyecto en RAM y lo escanea por coseno. Funciona bien en proyectos de tamaño normal; para monorepos enormes, indexa por subproyecto (`IndexOptions.Project`).
- **Equipo**: `FileStore` es de un solo usuario. Para un equipo usa un store compartido, y versiona `docs/` en git (es la parte que de verdad vale para todos).

Ejemplos: `go run ./examples/ciam/project_context` y `.../agent_with_context`.

---

Anterior: [17. Agent Protocol](17-agent-protocol.md) · Siguiente: [19. Open SWE](19-open-swe.md) · [Índice del manual](README.es-ES.md)
