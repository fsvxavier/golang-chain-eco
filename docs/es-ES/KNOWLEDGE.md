# Knowledge: RAG, CAG y KAG

[English](../en-US/KNOWLEDGE.md) | [Português](../pt-BR/KNOWLEDGE.md) | **Español**

Referencia de los paquetes `knowledge`, `rag`, `cag` y `kag`.
El código de la biblioteca usa solo la stdlib; el modelo, el embedder, el vector store y el grafo entran por interfaz.

## Índice

- [`knowledge`: contrato común](#knowledge-contrato-común) y [métricas de caché](#métricas-de-caché-en-messagesusage)
- [`rag`: indexador y pipeline](#rag-indexador-y-pipeline)
- [`cag`: corpus en prefijo cacheable](#cag-corpus-en-prefijo-cacheable)
- [`kag`: grafo de conocimiento](#kag-grafo-de-conocimiento): modelo, `GraphStore`, extracción de tripletas, `Engine`
- [`knowledge.Router`](#knowledgerouter-elección-de-la-técnica)
- [`knowledge.Toolkit`: tools y MCP](#knowledgetoolkit-tools-para-agents-y-servidor-mcp)
- [`integrations/graphstore`: Neo4j y Memgraph](#integrationsgraphstore-grafo-en-neo4j-y-memgraph)
- [Qué base de datos combinar con cada técnica](#qué-base-de-datos-combinar-con-cada-técnica)
- [`cmd/knowledge`: línea de comandos](#cmdknowledge-línea-de-comandos)

Para el paso a paso con recetas, véase el [capítulo 20 del manual](manual/20-rag-cag-kag.md).

## `knowledge`: contrato común

Toda técnica implementa `knowledge.Answerer`:

```go
type Answerer interface {
    Answer(ctx context.Context, q Question) (Answer, error)
}
```

| Tipo | Campos | Uso |
|---|---|---|
| `Question` | `Text`, `History []messages.Message`, `Filter vectorstores.Filter`, `Force string` | `Force` vale `""` (automático), `"rag"`, `"cag"` o `"kag"` |
| `Answer` | `Text`, `Sources []Source`, `Strategy`, `Usage *messages.Usage`, `Trace []Step` | `Strategy` dice quién respondió; `Trace` registra las decisiones |
| `Source` | `ID`, `Content`, `Metadata`, `Score` | evidencia usada en la respuesta |

Funciones auxiliares:

- `knowledge.AsRunnable(a)` expone un `Answerer` como `runnables.Runnable[Question, Answer]`.
- `knowledge.SourcesFromDocs(docs)` convierte documentos recuperados en `Source` (el `Score` sale de `metadata["score"]` cuando es `float64`).
- `knowledge.Prompt(system, fuentes, q)` arma los mensajes: system con el contexto numerado (`[1]`, `[2]`...), historial y la pregunta.
  `knowledge.DefaultSystem` pide responder solo con base en el contexto y citar `[n]`. `Prompt` no cambia un `system` vacío por `DefaultSystem`.
- `knowledge.ErrEmptyQuestion` se devuelve para una pregunta sin texto.

### Conteo de tokens

`knowledge.TokenCounter` (`Count(text string) int`) es sustituible. El predeterminado `knowledge.Heuristic{CharsPerToken: 4}` cuenta runas
y redondea hacia arriba (`""` da 0). Es una estimación, no un tokenizer: usa un contador propio cuando el margen importe.

## Métricas de caché en `messages.Usage`

`messages.Usage` tiene dos campos nuevos, aditivos (`omitempty` en JSON):

| Campo | Significado |
|---|---|
| `CacheReadTokens` | tokens de entrada leídos de la caché de prompt |
| `CacheCreationTokens` | tokens de entrada escritos en la caché (solo Anthropic lo informa) |

`InputTokens` sigue sumando todo, incluidos los tokens de caché, así que el código existente no cambia. `Usage.Add` suma los dos campos.
Relleno por proveedor:

- Anthropic: `cache_read_input_tokens` y `cache_creation_input_tokens`.
- OpenAI y compatibles: `prompt_tokens_details.cached_tokens` va a `CacheReadTokens`.
- Los demás proveedores dejan los campos en cero.

## `rag`: indexador y pipeline

### `Indexer`

```go
idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
stats, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil))
// stats.Added, stats.Skipped, stats.Removed (contados en chunks)
```

- Agrupa los documentos por `metadata["source"]` (un documento sin `source` obtiene su propio grupo, por posición) y calcula el sha256 del contenido de cada grupo.
- Una fuente sin cambios se omite (`Skipped`). Una fuente modificada tiene sus chunks antiguos borrados del store (`Removed`) y se reindexa (`Added`).
- Cada chunk recibe `source`, `chunk` y `content_hash`; el splitter ya informa `start_index`.
- `Documents()` devuelve todos los chunks indexados, ordenados por fuente: es la entrada del lado BM25 del pipeline.
- Seguro para uso concurrente.

### `Pipeline`

```go
p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, MultiQueries: 2, Reranker: rr, MaxContextTokens: 3000,
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "..."})
```

| Campo | Efecto |
|---|---|
| `K` (4) | fuentes devueltas |
| `FetchK` | candidatos buscados antes del rerank y del corte en `K` |
| `Hybrid` | fusiona la búsqueda vectorial con BM25 sobre `Docs` mediante RRF (`retrievers.Ensemble`) |
| `MultiQueries` | reformulaciones extra de la pregunta por el modelo (`retrievers.MultiQuery`) |
| `Reranker` | reordena los candidatos (`retrieval.WithRerank`, por ejemplo el rerank de Cohere) |
| `MaxContextTokens` | recorta fuentes desde el final hasta que quepan, midiendo con `Counter` (`nil` = `knowledge.Heuristic`) |
| `System` | reemplaza `knowledge.DefaultSystem` |

- `Question.Filter` aplica a la búsqueda vectorial y a BM25 (se aplica después de puntuar).
- Sin fuentes, el pipeline devuelve una respuesta vacía **sin llamar al modelo**, y el `Trace` dice `generate: skipped: no sources`.
- `Retrieve(ctx, q)` devuelve solo las fuentes, sin generar. Rechaza un `Question.Force` distinto de `""` y `"rag"`.
- `Docs` es una copia del momento de la creación: después de reindexar, vuelve a armar el `Pipeline` o asigna `Docs = idx.Documents()`.

### Límites

- El estado de hashes del `Indexer` vive solo en la memoria del proceso; al reiniciar se reindexa todo. Las fuentes que desaparecen del loader no se eliminan del store.
- El índice BM25 se recrea en cada pregunta. Para corpus grandes y estables, eso debería pasar a ser caché.
- Los embeddings no pasan por `embeddings.Cached` automáticamente; envuelve el embedder del store si el costo importa.

## `cag`: corpus en prefijo cacheable

CAG pone el corpus completo en el mensaje `system` y deja que la caché de prompt del proveedor haga el resto. No hay búsqueda en tiempo de ejecución.

### `Corpus`

- `cag.NewCorpus(docs)` ordena los documentos por `metadata["source"]`, luego por `ID` y por contenido, así que el orden de entrada no cambia el prefijo.
- `Text()` arma bloques `<document source="...">...</document>`; `Hash()` es el sha256 de ese texto; `Tokens(counter)` estima el tamaño con un `knowledge.TokenCounter`
  (`nil` usa `knowledge.Heuristic`); `Documents()` devuelve los documentos ya ordenados.
- Los métodos aceptan un `*Corpus` nulo.

### `Engine`

```go
e := &cag.Engine{
    Model:    model,
    Corpus:   cag.NewCorpus(docs),
    Window:   200000, // ventana del modelo en tokens (predeterminado)
    Reserve:  4096,   // margen para historial, pregunta y respuesta (predeterminado)
    Fallback: ragPipeline, // opcional: cualquier knowledge.Answerer
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "..."})
```

Orden de los mensajes: `system` (instrucción + corpus), luego `Question.History`, luego la pregunta. El historial nunca va antes del corpus; si no, el prefijo cambiaría en cada turno y la caché no acertaría.

| Situación | Resultado |
|---|---|
| corpus + `Reserve` caben en `Window` | respuesta con `Strategy == "cag"`; `Trace` trae `corpus_hash` |
| la respuesta trae `Usage.CacheReadTokens > 0` | `Trace` gana el paso `cache_hit` |
| no cabe y hay `Fallback` | el `Fallback` responde; `Trace` empieza con el paso `fallback` y el motivo |
| no cabe y no hay `Fallback` | `cag.ErrCorpusTooLarge` |
| corpus vacío | `cag.ErrEmptyCorpus` |
| pregunta vacía | `knowledge.ErrEmptyQuestion` |

`Engine.SetCorpus(c)` cambia el corpus y devuelve `true` cuando cambió el hash, es decir, cuando se reconstruirá la caché del proveedor.
Es seguro llamarlo junto con `Answer`.

### Marcado del prefijo (`PrefixCacher`)

| Implementación | Efecto | Usada para |
|---|---|---|
| `cag.AnthropicCache` | envía el system como un bloque con `cache_control: {type: ephemeral}` | modelos `anthropic:*` |
| `cag.StablePrefix` | no altera el mensaje; solo garantiza un prefijo idéntico byte a byte | OpenAI y compatibles (caché automática por prefijo) |

`cag.CacherFor(modelID)` elige por el prefijo del id del modelo; `Engine.Cacher` lo sobrescribe. Los proveedores sin caché de prompt funcionan,
solo que no ahorran: el ahorro aparece en `Usage.CacheReadTokens`.

### Límites

- El conteo de tokens es la heurística de `knowledge.Heuristic` (~4 caracteres por token). Un corpus cercano a la ventana merece un contador propio.
- CAG no tiene estado de caché local: depende de la caché del proveedor, que expira (minutos en Anthropic) y cobra la primera escritura.

## `kag`: grafo de conocimiento

### Modelo

| Tipo | Campos |
|---|---|
| `Entity` | `ID`, `Type`, `Name`, `Props map[string]any`, `Vector []float32` (opcional, habilita la búsqueda por coseno) |
| `Relation` | `From`, `Type`, `To`, `Props`, `Source` (chunk o documento de origen) |
| `Direction` | `Out`, `In`, `Both` |

`kag.NormalizeID(s)` pasa a minúsculas, recorta y cambia los espacios por `_`.

### `GraphStore`

```go
type GraphStore interface {
    UpsertEntities(ctx, []Entity) error
    UpsertRelations(ctx, []Relation) error
    Neighbors(ctx, id string, relTypes []string, dir Direction) ([]Relation, error)
    FindEntities(ctx, text string, vec []float32, k int) ([]Entity, error)
    Entity(ctx, id string) (Entity, bool, error)
    Path(ctx, from, to string, maxHops int) ([][]Relation, error)
    Delete(ctx, ids []string) error
}
```

`kag.NewMemory()` es la implementación de referencia, segura para uso concurrente. `Save(path)` y `kag.LoadMemory(path)` persisten en JSON.
Comportamiento que cualquier adapter debe repetir:

- `UpsertRelations` con el mismo `(From, Type, To)` reemplaza `Props` y `Source`.
- `Path` sigue solo aristas de salida y devuelve los caminos **más cortos**; devuelve `nil` si `from == to` o `maxHops <= 0`. Termina en grafos con ciclos.
- `Delete` elimina las entidades y todas las relaciones que las tocan.
- `FindEntities` usa coseno cuando existen `vec` y `Entity.Vector`; si no, la fracción de los términos de la consulta presentes en `Name` o `ID`. Un score 0 queda fuera y el empate sale por `ID`.
- `Neighbors` devuelve en orden estable (`From|Type|To`).

La suite `conformance.Graph(t, store)` (paquete `internal/conformance`) verifica ese contrato y debe ejecutarse contra todo adapter nuevo.

### Extracción de tripletas (`Extractor`)

```go
x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCRIBIO", "INTERACTUA_CON"}}}
stats, err := x.Extract(ctx, chunks) // ExtractStats{Entities, Relations, Dropped}
```

Para cada documento, el modelo devuelve, mediante salida estructurada, `{entities:[{name,type}], relations:[{from,type,to}]}`.

- Los IDs vienen de `NormalizeID(name)`. Una relación con un extremo que el modelo no listó crea la entidad.
- `Relation.Source` es el `ID` del documento o el `metadata["source"]`, para rastrear de dónde salió cada tripleta.
- Los vacíos y duplicados se descartan y se cuentan en `Dropped`. Un documento en blanco se omite sin llamar al modelo.
- `Embedder` es opcional: sin él, las entidades no tienen vector y la búsqueda cae al nombre. Si el embedder falla, no se escribe nada y el error vuelve.
- Un chunk con salida inválida no derriba el lote: los errores se unen (`errors.Join`, el mensaje cita `chunk N`) y los chunks buenos se escriben.
- `Schema.RelationTypes` filtra los tipos aceptados. `Schema.EntityTypes` solo entra en el prompt, no se impone.

**Seguridad.** El tipo de relación viene de un LLM y después se convierte en tipo de relación en Cypher (Neo4j y Memgraph), donde no puede ser un parámetro.
El extractor normaliza todo tipo a `^[A-Z][A-Z0-9_]*$`: mayúsculas, cualquier otro carácter se vuelve `_`, las repeticiones colapsan, el `_` de los extremos sale,
y un tipo vacío o que empieza por dígito se descarta. `tratar con; DROP` se convierte en `TRATAR_CON_DROP`. La normalización es solo ASCII, así que los acentos se vuelven `_`.

### `Engine`: planificación multi-salto

```go
e := &kag.Engine{Model: model, Graph: g, Embedder: emb, Chunks: ragPipeline, Verify: true}
ans, err := e.Answer(ctx, knowledge.Question{Text: "¿Qué pacientes tratados por el Dr. Silva tuvieron interacción medicamentosa?"})
```

Flujo de una pregunta:

1. **Plan.** El modelo devuelve, mediante salida estructurada, un `kag.Plan` con hasta `MaxSteps` pasos (predeterminado 4). Cada `PlanStep` tiene `ID`, `Goal`, `Entity`
   (nombre de la entidad de partida, o `"$<id>"` para partir de las entidades alcanzadas por un paso anterior), `Relations` (tipos a seguir; vacío = todos),
   `Dir` (`out`, `in` o `both`, predeterminado `both`) y `DependsOn`. Una referencia a un paso desconocido o posterior es un error.
2. **Pasos.** Cada paso liga la entidad al grafo (`FindEntities`, con vector si hay `Embedder`) y recorre `Neighbors` hasta `MaxHops` saltos (predeterminado 3).
   El recorrido guarda las entidades visitadas, así que termina en grafos con ciclos, y un conjunto global de tripletas evita evidencia repetida entre pasos.
3. **Complemento por chunks.** Si un paso no trae ninguna tripleta nueva y `Chunks` está definido, las fuentes de `Chunks.Answer` entran como evidencia (solo las fuentes, no el texto de la respuesta).
4. **Síntesis.** El modelo responde con la evidencia numerada (`knowledge.Prompt`). Cada tripleta es una `Source` con `ID` `De|TIPO|Para`, contenido `de —TIPO→ para`
   y `Metadata["source"]` con el chunk de origen.
5. **Verificación** (`Verify: true`). Una segunda llamada comprueba si la respuesta está sustentada por la evidencia. Si no lo está, la respuesta gana el prefijo
   `(no verificado)` y el `Trace` gana el paso `unverified` con lo que faltó. Un fallo de la propia llamada de verificación es un error, no una advertencia.

El `Trace` tiene un paso `step:<id>` por paso del plan, con las tripletas encontradas. `Strategy` es `"kag"`. `Usage` es solo el de la llamada de síntesis
(plan y verificación no entran en la suma). Cada etapa emite eventos de `callbacks`, así que `tracing` y el stream de LangGraph ven el plan y los pasos.

#### Límites

- Un paso cuyas tripletas ya fueron recogidas por un paso anterior aparece como `sin tripletas en el grafo` y puede activar el complemento por chunks sin necesidad.
- Un paso recorre hasta `MaxHops` saltos de una vez; el planificador no está obligado a dar un salto por paso.
- El `Path` del `GraphStore` no lo usa el engine hoy; queda disponible para tools de explicación de caminos.
- La calidad del plan depende del modelo. Con modelos pequeños, prefiere `Schema.RelationTypes` en el extractor para que el vocabulario sea predecible.

## `knowledge.Router`: elección de la técnica

```go
r := &knowledge.Router{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine,
    Profile:  knowledge.Profile{CorpusTokens: 40000, ContextWindow: 200000, Volatile: false, HasGraph: true},
    Fallback: true,
}
ans, err := r.Answer(ctx, knowledge.Question{Text: "¿Quién prescribió el medicamento X?"})
```

El router también es un `Answerer`; cualquiera de las tres técnicas puede quedar en `nil`. Las reglas, en orden:

1. `Question.Force` (`"rag"`, `"cag"`, `"kag"`) gana. Si la técnica forzada no está configurada, el resultado es `knowledge.ErrNoStrategy`, sin caer en las reglas. Un valor inválido es un error.
2. Hay grafo (`Profile.HasGraph`), `KAG` configurado y el `Classifier` dice que la pregunta es multi-salto: **KAG**. El clasificador solo se ejecuta en este caso.
3. Corpus estable (`!Volatile`), `CorpusTokens > 0` y `CorpusTokens <= ContextWindow * 0.8`, con `CAG` configurado: **CAG**.
4. Si no: **RAG**.

Si la técnica elegida no existe, el router usa la primera disponible en el orden RAG, CAG, KAG y lo dice en el motivo. Sin ninguna: `ErrNoStrategy`.

`Route(ctx, q)` devuelve solo `(estrategia, motivo, error)`, sin responder. En `Answer`, el primer paso del `Trace` es `route` (`"<estrategia> — <motivo>"`),
y `Answer.Strategy` es la técnica que de hecho respondió. Con `Fallback: true`, un error de la técnica elegida (que no sea RAG) repite la pregunta en RAG,
y el `Trace` gana el paso `fallback` con el error original.

### Clasificadores de multi-salto

| Tipo | Cómo decide |
|---|---|
| `KeywordClassifier` (predeterminado) | busca pistas de relación, sin distinguir mayúsculas ni acentos: `quien`, `entre`, `relaciona*`, `dependencia`, `camino`, `who`, `between`, `related`, `path`, `depends` |
| `LLMClassifier{Model}` | hace una pregunta sí/no al modelo; una respuesta fuera de `sí/no/yes/no` se convierte en error |

Implementa `knowledge.Classifier` (`MultiHop(ctx, Question) (bool, error)`) para una regla propia. Un fallo del clasificador interrumpe el enrutamiento con error.

### Límite

Las pistas de `KeywordClassifier` son heurísticas: "quién" también aparece en preguntas simples. Para precisión, usa `LLMClassifier` o un clasificador propio.

## `knowledge.Toolkit`: tools para agents y servidor MCP

```go
tk := knowledge.Toolkit{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine, Router: router,
    Ingest: func(ctx context.Context, docs []documents.Document) error { return miIngest(ctx, docs) }, // indexa los documentos en tu RAG/KAG
}
agent, _ := agents.Create(agents.Options{Model: model, Tools: tk.All()}) // tools en proceso
srv := tk.MCPServer(nil)                                                 // las mismas tools para editores
```

`All()` solo incluye la tool cuyo `Answerer` está definido:

| Tool | Existe cuando | Argumentos | Hace |
|---|---|---|---|
| `rag_answer` | `RAG != nil` | `question`, `force` (opcional) | responde buscando en los fragmentos |
| `cag_answer` | `CAG != nil` | igual | responde con el corpus en contexto |
| `kag_query` | `KAG != nil` | igual | responde recorriendo el grafo |
| `knowledge_answer` | `Router != nil` | igual | el router elige la técnica (descripción marcada `[PRIORITY]`) |
| `knowledge_ingest` | `Ingest != nil` | `documents` (lista de `{id?, content, metadata?}`) | llama a tu `Ingest` |

- El retorno es `tools.ContentAndArtifact`: el modelo ve el texto con las fuentes numeradas (`[n] id`), y el `Artifact` guarda el `knowledge.Answer` completo (`Trace`, `Usage`).
- La lista `documents` llega como `[]any` (regla de schema de las tools); en las pruebas pasa `[]any`, no `[]string`.
- El error de una técnica se convierte en error de tool que el modelo ve (`tools.Errorf`), sin derribar el agent.
- `MCPServer(onCall)` devuelve un `server.Server` llamado `knowledge`, con instrucciones de uso, en el mismo formato que `ciam.Toolkit.MCPServer`. Sírvelo por stdio (`ServeStdio`) o HTTP (`Handler`).
  `go run ./examples/knowledge/mcp_server -demo` muestra un `tools/list` y un `tools/call` en proceso.

### Como nodo de LangGraph

```go
g.AddNode("responder", knowledge.Node(router, "question", "answer"))
```

`knowledge.Node(a, questionKey, answerKey)` lee `state[questionKey]` (string) y escribe `state[answerKey]` con el texto y `state[answerKey+"_sources"]` con las fuentes.

## `integrations/graphstore`: grafo en Neo4j y Memgraph

Dos adapters de `kag.GraphStore`, ambos en Cypher. Cambia `kag.NewMemory()` por uno de ellos y el resto (extractor, engine, tools) sigue igual.

```go
// Neo4j 5.19+ o Aura, mediante la Query API HTTP
g := &graphstore.Neo4j{URL: "http://localhost:7474", Username: "neo4j", Password: os.Getenv("NEO4J_PASS"), Database: "neo4j", Label: "Entity"}

// Memgraph, mediante el cliente Bolt del repositorio
g := &graphstore.Memgraph{Client: &memgraph.Client{Addr: "localhost:7687"}, Label: "Entity"}
```

`Label` (predeterminado `Entity`) separa grafos en la misma base de datos: usa una etiqueta por base de conocimiento.

### Modelo en la base de datos

- Entidad: nodo `:<Label>` con `id`, `name`, `type`, `props` (JSON en texto), `vector` (lista de floats) y `entity = true`. `id` tiene una restricción de unicidad, creada en la primera escritura.
- Relación: arista dirigida cuyo **tipo** es el `Relation.Type`; `props` y `source` quedan en la arista. Una relación hacia una entidad todavía inexistente crea un nodo provisional (`entity = false`),
  ignorado por `Entity()` y `FindEntities()`; cuando llega la entidad, el nodo se completa. `kag.Memory` no tiene nodos provisionales.
- `Upsert` usa `MERGE`: repetir la escritura es idempotente.

### Seguridad

El tipo de relación y la etiqueta no pueden ser parámetros en Cypher, así que se interpolan en el texto de la consulta. Por eso:

- el tipo de relación debe coincidir con `^[A-Z][A-Z0-9_]*$` (el extractor ya normaliza así); cualquier otro valor devuelve un error **antes** de cualquier solicitud a la base de datos;
- `Label` debe coincidir con `^[A-Za-z_][A-Za-z0-9_]*$`;
- el límite de saltos de `Path` es un entero validado y positivo;
- todos los valores (ids, nombres, props, vectores) van como parámetros, nunca en el texto.

### Diferencias entre los dos

| | Neo4j | Memgraph |
|---|---|---|
| Transporte | HTTP, `POST /db/<db>/query/v2`, basic auth | Bolt 4.x (`integrations/memgraph`) |
| `Path` | `allShortestPaths((a)-[*1..N]->(b))` | `(a)-[*ALLSHORTEST ..N (r, n \| 1) w]->(b)` |
| Fila malformada en `Neighbors` | ignorada | error |

En Memgraph, `allShortestPaths(...)` no existe y `*BFS` devuelve solo uno de los caminos mínimos; `*ALLSHORTEST` devuelve todos (verificado en un grafo con dos caminos de igual longitud).

### Límites

- `FindEntities` lee las entidades de la etiqueta y puntúa en Go (coseno cuando hay vector en ambos lados, si no fracción de los términos en el nombre), para tener el mismo resultado que `kag.Memory`.
  Funciona bien hasta algunas decenas de miles de entidades; por encima, usa el índice vectorial de la base de datos (`vector.similarity.cosine` en Neo4j, índice vectorial en Memgraph) en una evolución futura.
- No hay transacciones entre `UpsertEntities` y `UpsertRelations`: un fallo entre las dos deja entidades sin sus relaciones. Volver a ejecutar la extracción es seguro (idempotente).
- Neo4j se habla solo por HTTP (Query API); Bolt para Neo4j no está implementado.

### Pruebas contra las bases de datos reales

Las pruebas de contrato (`httptest`, validación de identificadores, conversión de filas) se ejecutan siempre. Las pruebas reales se omiten solas sin la variable de entorno y pasan
la suite `conformance.Graph`, la misma de `kag.Memory`:

```bash
docker run -d --name lcg-neo4j -p 7474:7474 -p 7688:7687 -e NEO4J_AUTH=neo4j/lcgtestpass neo4j:5.26
docker run -d --name lcg-memgraph -p 7687:7687 memgraph/memgraph

NEO4J_URL=http://127.0.0.1:7474 NEO4J_USER=neo4j NEO4J_PASS=lcgtestpass \
MEMGRAPH_ADDR=127.0.0.1:7687 go test -race -count=1 -v ./integrations/graphstore
```

Cada ejecución usa una etiqueta única y borra lo que creó.

## Qué base de datos combinar con cada técnica

| Técnica | Necesita | Opciones en el repositorio |
|---|---|---|
| RAG | un `vectorstores.VectorStore` | `vectorstores.Memory`; cualquier `Backend` de `integrations/vectorstore` envuelto por `vectorstore.New(embedder, backend)` (`Flat`, `Annoy`, `HNSW`, pgvector, Qdrant, Milvus, Pinecone, Weaviate, Chroma, Elasticsearch, Neo4j, Memgraph y los demás de la lista del capítulo 13) |
| CAG | solo el modelo; la caché es del proveedor | Anthropic (`cache_control`), OpenAI y compatibles (caché automática por prefijo) |
| KAG | un `kag.GraphStore` y, opcionalmente, un `VectorStore` para el `Chunks` | `kag.Memory`; adapters de bases de datos de grafos en la fase de integraciones |

RAG es agnóstico al backend porque solo usa la interfaz `VectorStore`. La suite `conformance.RAG(t, newStore)` (`internal/conformance/rag.go`) lo demuestra: indexa
cuatro fuentes, comprueba la recuperación, el filtro por metadato, la reindexación sin cambios (`Skipped`), la edición de una fuente (`Removed` + `Added`, el texto antiguo
desaparece) y la respuesta con fuentes. Hoy se ejecuta sobre `vectorstores.Memory`, `Flat` y `HNSW`; al añadir un backend, llama a la suite con un constructor suyo:

```go
func TestMiBackend(t *testing.T) {
    conformance.RAG(t, func() vectorstores.VectorStore { return vectorstore.New(emb, miBackend()) })
}
```

## `cmd/knowledge`: línea de comandos

```bash
go install ./cmd/knowledge

knowledge index docs --glob "*.md" --kind both --out .knowledge   # vectors.json (RAG) y graph.json (KAG)
knowledge ask "¿Cuál es la garantía del Zefiro X200?" --dir .knowledge   # el router elige la técnica
knowledge ask "..." --mode cag --src docs                          # CAG: la carpeta se vuelve el corpus en contexto
knowledge mcp --dir .knowledge                                     # servidor MCP en stdio (editores)
knowledge serve --addr :8080 --dir .knowledge                      # MCP sobre HTTP
```

| Comando | Hace |
|---|---|
| `index <path> [--glob] [--kind rag\|kag\|both] [--out]` | carga los archivos, indexa (RAG) y/o extrae el grafo (KAG) y escribe en `--out` (predeterminado `./.knowledge`) |
| `ask "pregunta" [--mode auto\|rag\|cag\|kag] [--dir] [--src] [--glob]` | responde; imprime el texto, las fuentes (`[n] id  origen`) y el `trace:` |
| `mcp [--dir]` / `serve [--addr] [--dir]` | `knowledge.Toolkit.MCPServer` por stdio o HTTP |

- `--mode auto` usa el `knowledge.Router`: hay grafo cuando existe `graph.json`; CAG solo entra si se indica `--src`. Solo en el modo `auto` hay fallback a RAG.
- `kag` sin `graph.json` y `cag` sin `--src` fallan con un mensaje que dice qué hacer.
- Modelo: `KNOWLEDGE_MODEL` (por ejemplo `anthropic:claude-sonnet-5-5`); sin él, `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` eligen un predeterminado; sin clave, un modelo offline que repite el primer fragmento de contexto, con aviso. Embeddings: `KNOWLEDGE_EMBEDDINGS` (por ejemplo `ollama:nomic-embed-text`); el predeterminado es `embeddings.Fake`.
- El modo offline sirve para probar el flujo. La planificación de KAG exige un modelo real (`KNOWLEDGE_MODEL`).

### Límites del CLI

- `index` siempre reconstruye desde cero; no hay indexación incremental entre ejecuciones.
- La búsqueda del CLI es solo vectorial (K=4): `vectors.json` no guarda la lista de documentos que necesita BM25.
- El servidor MCP del CLI no expone `knowledge_ingest`.
- El grafo del CLI es el `kag.Memory` en `graph.json`; para Neo4j o Memgraph, arma el `kag.Engine` en código.
