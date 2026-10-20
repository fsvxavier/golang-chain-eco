# 20. RAG, CAG y KAG

[English](../../en-US/manual/20-rag-cag-kag.md) | [Português](../../pt-BR/manual/20-rag-cag-kag.md) | **Español**

Tres formas de dar conocimiento a un modelo, con la misma interfaz: `knowledge.Answerer` recibe una pregunta y devuelve texto, fuentes, la estrategia usada y un rastro (`Trace`) de las decisiones.
Este capítulo muestra cuándo usar cada una, cómo armarla, cómo dejar que el router elija y cómo conectarlo todo a agents y MCP.
Referencia de los tipos: [KNOWLEDGE.md](../KNOWLEDGE.md).

## 20.1 Cuál usar

| | RAG | CAG | KAG |
|---|---|---|---|
| Idea | busca los fragmentos más cercanos y los inyecta en el prompt | pone el corpus entero en el prompt y deja que el proveedor lo cachee | extrae un grafo de entidades y relaciones y razona por saltos |
| Paquete | `rag` | `cag` | `kag` |
| Escala de datos | alta (millones de fragmentos) | baja (cabe en la ventana del modelo) | media a alta (depende del modelado) |
| Latencia por pregunta | media | baja (prefijo en caché) | mayor (plan, saltos, síntesis) |
| Pregunta multi-salto | débil si la respuesta está en fragmentos separados | buena, el modelo lo ve todo | el mejor caso |
| Actualizar datos | fácil (reindexar la fuente) | cambia el corpus y pierde la caché | reextraer los fragmentos modificados |
| Costo por pregunta | medio | bajo con caché, alto sin ella | más llamadas al modelo |

Regla práctica: miles de documentos o una base que cambia constantemente: **RAG**. Pocos documentos estables y latencia mínima: **CAG**. Preguntas del tipo "quién se relaciona con quién" o "cuál es la causa raíz": **KAG**. En la duda, `knowledge.Router` decide por ti (sección 20.5).

## 20.2 RAG

```go
emb := embeddings.Fake{Dim: 256} // cámbialo por un embedder real
store := vectorstores.NewMemory(emb) // o cualquier VectorStore de integrations/vectorstore

idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
if _, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil)); err != nil { /* ... */ }

p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, // vector + BM25 por RRF
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "¿Cómo restablezco la contraseña del Router-X500?"})
fmt.Println(ans.Text)
for _, s := range ans.Sources { fmt.Println(s.Metadata["source"]) }
```

- La respuesta cita las fuentes como `[1]`, `[2]`, en el orden de `ans.Sources`.
- `Hybrid` encuentra términos exactos (códigos de error, nombres de producto) que la búsqueda vectorial pierde. Mira `go run ./examples/knowledge/rag`.
- Reindexar es barato: una fuente con el mismo contenido se omite (`Skipped`); una modificada reemplaza solo sus propios fragmentos.
- ¿Quieres reordenar con un modelo de rerank? Rellena `Reranker` con el resultado de `chatmodels.NewReranker("cohere:<modelo>")` (también `jina:` y `voyageai:`).
- Filtrar por metadato: `knowledge.Question{Filter: vectorstores.MetadataEquals(map[string]any{"producto": "x500"})}`.

Combínalo con cualquier backend vectorial del capítulo 13 (pgvector, Qdrant, Milvus, Chroma, Elasticsearch, Neo4j, Memgraph...): el `Pipeline` solo ve un `vectorstores.VectorStore`.

## 20.3 CAG

```go
corpus := cag.NewCorpus(docs) // el orden de entrada no importa
e := &cag.Engine{
    Model:    chatmodels.MustInit("anthropic:claude-sonnet-5-5"),
    Corpus:   corpus,
    Window:   200000,
    Fallback: p, // el pipeline RAG de arriba, usado si el corpus no cabe
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "¿Cuál es la multa de rescisión con el Proveedor Y?"})
```

- El corpus va en el mensaje `system` y el historial después, para que el prefijo quede idéntico entre preguntas y la caché del proveedor acierte.
- Anthropic: el bloque recibe `cache_control: ephemeral`. OpenAI y compatibles: caché automática por prefijo. Otros proveedores funcionan, sin ahorro.
- Para confirmar que la caché funcionó, mira `ans.Usage.CacheReadTokens` o el paso `cache_hit` en el `Trace`.
- Corpus modificado: `e.SetCorpus(nuevo)` devuelve `true` y la caché del proveedor se reconstruirá en la siguiente llamada.
- Corpus por encima de `Window - Reserve`: usa el `Fallback`; sin `Fallback`, `cag.ErrCorpusTooLarge`.

El conteo de tokens es una estimación (~4 caracteres por token). Cerca del límite, inyecta un `knowledge.TokenCounter` propio en `Engine.Counter`.

## 20.4 KAG

```go
g := kag.NewMemory() // o &graphstore.Neo4j{...} / &graphstore.Memgraph{...} para una base de datos de grafos

x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCRIBIO", "INTERACTUA_CON", "PRESCRITA_A"}}}
stats, err := x.Extract(ctx, chunks) // chunks: []documents.Document

e := &kag.Engine{Model: model, Graph: g, Embedder: emb, Chunks: p, Verify: true}
ans, err := e.Answer(ctx, knowledge.Question{
    Text: "¿Quién prescribió un medicamento que interactúa con la Aspirina prescrita a Juan?"})
for _, st := range ans.Trace { fmt.Println(st.Name, st.Detail) } // una línea por salto
```

- El extractor convierte cada fragmento en entidades y relaciones; cada tripleta guarda el fragmento de origen. Los vacíos y duplicados se descartan.
- El engine planifica la pregunta en pasos, recorre el grafo (sin repetir tripletas, incluso con ciclos) y sintetiza con la evidencia numerada. Las fuentes de la respuesta son las tripletas usadas.
- `Verify: true` contrasta la respuesta con la evidencia y marca `(no verificado)` cuando algo no está sustentado.
- `Chunks` (un `Answerer` de RAG) completa los pasos en los que el grafo no tiene nada.
- Usa `Schema.RelationTypes` para que el vocabulario de relaciones sea estable; el tipo se normaliza a `MAYÚSCULAS_CON_GUION_BAJO` antes de llegar al grafo.

`go run ./examples/knowledge/kag` muestra la búsqueda vectorial sola fallando la pregunta de 3 saltos y el engine respondiéndola.

## 20.5 Router

```go
r := &knowledge.Router{
    RAG: p, CAG: e1, KAG: e2,
    Profile:  knowledge.Profile{CorpusTokens: 40000, ContextWindow: 200000, Volatile: false, HasGraph: true},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: "¿Quién se relaciona con el contrato Y?"})
fmt.Println(ans.Strategy, ans.Trace[0]) // kag, route: kag — pregunta multi-salto con grafo disponible
```

El primer paso del `Trace` es siempre la decisión (`route`). Puedes forzar la técnica por pregunta con `Question.Force`. Orden de las reglas: Force, luego multi-salto con grafo (KAG), luego corpus estable que cabe en el 80% de la ventana (CAG), si no RAG. Con `Fallback: true`, un error en CAG o KAG repite la pregunta en RAG, y el `Trace` lo registra.
El clasificador de multi-salto predeterminado es una heurística de palabras (pt-BR y en); usa `knowledge.LLMClassifier{Model: m}` cuando necesites más precisión.

## 20.6 Integraciones

**Tools para agents y MCP.** Un `knowledge.Toolkit` se convierte en tools (`rag_answer`, `cag_answer`, `kag_query`, `knowledge_answer`, `knowledge_ingest`) para `agents.Create`
y en un servidor MCP para editores, con las mismas tools:

```go
tk := knowledge.Toolkit{RAG: p, CAG: e1, KAG: e2, Router: r}
agent, _ := agents.Create(agents.Options{Model: model, Tools: tk.All()})
srv := tk.MCPServer(nil) // srv.ServeStdio(ctx, os.Stdin, os.Stdout)
```

`go run ./examples/knowledge/agent_tools` muestra al agent llamando a `rag_answer`; `go run ./examples/knowledge/mcp_server -demo` muestra `tools/list` y `tools/call` en proceso.
Dentro de un grafo de LangGraph usa `knowledge.Node(r, "question", "answer")`.

**Bases de datos de grafos.** `graphstore.Neo4j` y `graphstore.Memgraph` implementan `kag.GraphStore`; cambiar `kag.NewMemory()` por ellos no cambia nada más. Los tipos de relación se validan
(`^[A-Z][A-Z0-9_]*$`) antes de entrar en la consulta Cypher. Detalles, diferencias y cómo probar contra Docker en [KNOWLEDGE.md](../KNOWLEDGE.md).

**Bases de datos vectoriales.** El RAG acepta cualquier `vectorstores.VectorStore`; `vectorstore.New(embedder, backend)` envuelve los backends del capítulo 13. La suite `conformance.RAG` garantiza el mismo comportamiento
en `Memory`, `Flat` y `HNSW`.

**Línea de comandos.**

```bash
go run ./cmd/knowledge index docs --kind both
go run ./cmd/knowledge ask "¿Quién se relaciona con el contrato Y?"
```

## 20.7 Recetas

- **Cada técnica detrás de una sola interfaz.** El código de la aplicación recibe un `knowledge.Answerer`; cambiar RAG por CAG no cambia nada más.
- **Como chain/Runnable:** `knowledge.AsRunnable(a)` devuelve un `runnables.Runnable[Question, Answer]`.
- **Medir el ahorro de caché:** suma `Usage.CacheReadTokens` de las respuestas; `InputTokens` sigue sumando todo.
- **Persistir:** `vectorstores.Memory.Save/Load` y `kag.Memory.Save/LoadMemory` guardan en JSON; en producción usa un backend vectorial real (capítulo 13).

## 20.8 Límites conocidos

- La indexación incremental del RAG guarda los hashes solo en la memoria del proceso; reiniciar reindexa todo. Las fuentes que desaparecen del loader no se eliminan del store.
- El BM25 del pipeline se recrea en cada pregunta (bien hasta unos miles de fragmentos).
- El CAG depende de la caché del proveedor, que expira y cobra la primera escritura; el conteo de tokens es estimado.
- En el KAG, la calidad del plan y de la extracción viene del modelo. Un paso cuyas tripletas ya fueron recogidas por otro puede activar el complemento por fragmentos sin necesidad.
- El `Usage` del KAG cubre solo la llamada de síntesis (el plan, la extracción y la verificación no entran en la suma).
- `FindEntities` de los adapters de Neo4j y Memgraph puntúa en Go después de leer las entidades de la etiqueta; para grafos muy grandes, falta usar el índice vectorial de la base de datos.

## 20.9 Problemas comunes

| Síntoma | Causa probable |
|---|---|
| `cag: corpus does not fit the context window` | corpus mayor que `Window - Reserve`; define `Fallback` o reduce el corpus |
| `Cache read` siempre en cero en Anthropic | el prefijo cambió entre llamadas (corpus modificado, instrucción distinta) o la caché expiró |
| KAG responde "no sé" | grafo vacío o extracción sin tripletas (mira `ExtractStats.Dropped`); define `Chunks` como complemento |
| `rag: cannot answer with strategy "kag"` | `Question.Force` apunta a otra técnica; deja que el router decida o limpia el campo |
| `kag needs graph.json` / `cag needs --src` (CLI) | el modo pedido necesita `index --kind kag` o `--src` |
| `graphstore: relation type ... must match` | tipo de relación fuera de `^[A-Z][A-Z0-9_]*$`; usa `kag.Extractor`, que lo normaliza |
| `knowledge: no strategy available` | `Router` sin ninguna técnica configurada, o `Force` a una técnica `nil` |

---

Anterior: [19. Open SWE](19-open-swe.md) · [Índice del manual](README.es-ES.md)
