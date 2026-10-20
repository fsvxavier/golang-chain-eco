# 14. Producción, seguridad y troubleshooting

[English](../../en-US/manual/14-producao-seguranca-troubleshooting.md) | [Português](../../pt-BR/manual/14-producao-seguranca-troubleshooting.md) | **Español**

## 14.1 Checklist de producción

**Persistencia**

- [ ] Checkpointer durable (`sqlsaver`, Redis, MongoDB, DynamoDB, S3), nunca `checkpoint.NewMemory()`.
- [ ] `thread_id` derivado del usuario/sesión autenticada, no aceptado en bruto del cliente (evita leer la conversación de otro).
- [ ] Tipos propios del estado registrados con `checkpoint.Register`.
- [ ] Política de retención: `saver.DeleteThread(ctx, id)` para borrar conversaciones antiguas.

**Costo y estabilidad**

- [ ] `ModelCallLimit` y `ToolCallLimit` en el agent.
- [ ] `RecursionLimit` explícito en `runnables.Config`.
- [ ] `ModelRetry`/`ToolRetry` y `ModelFallback` para errores transitorios.
- [ ] `Summarization` o `ContextEditing` para conversaciones largas.
- [ ] `models.NewRateLimiter` por proveedor, respetando las cuotas de la cuenta.
- [ ] `context.WithTimeout` en toda llamada que venga de una solicitud HTTP.

**Observabilidad**

- [ ] Un tracer activo (LangSmith, Langfuse u OTLP) con `defer Close()`.
- [ ] `OnError` del tracer conectado a un log.
- [ ] Uso de tokens y latencia por modelo/tool en métricas.

**Seguridad**

- [ ] Secretos en variables de entorno o en un gestor.
- [ ] Tools sensibles protegidas por `HumanInTheLoop`.
- [ ] `PII` activo cuando hay datos personales.
- [ ] Sin `LocalShell` en producción; usa `kubernetes.Sandbox` o un runner aislado.
- [ ] `Permissions` configuradas en los Deep Agents con disco real.

## 14.2 Cierre y contexto

Todo método recibe un `context.Context`. Cancelar el contexto interrumpe la llamada al proveedor, el streaming y el grafo. En servidores HTTP, propaga `r.Context()`:

```go
func handler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = runnables.WithConfig(ctx, graph.Thread(userThread(r)))
	out, err := agent.Invoke(ctx, graph.State{"messages": readBody(r)})
	...
}
```

Al apagar el proceso: deja de aceptar solicitudes, espera las que están en curso, luego llama a `Close()` de los tracers y cierra conexiones.

## 14.3 Concurrencia

- `*graph.Compiled`, los modelos y las tools son seguros para uso concurrente y deben **crearse una vez** y reutilizarse entre solicitudes.
- Los nodos de un grafo corren en goroutines: no compartas mapas o slices mutables entre nodos sin sincronización. El estado entregado a un nodo es una copia.
- `callbacks.Handler` debe ser seguro para concurrencia.
- Cada thread (`thread_id`) debe tener **una ejecución a la vez**. Dos llamadas simultáneas en el mismo thread compiten por el mismo checkpoint; serialízalas en tu código (una cola por thread) o usa `Multitask` en el servidor remoto.

## 14.4 Seguridad

### Ejecución de código

| Componente | Riesgo | Mitigación |
|---|---|---|
| `backends.LocalShell` | Ejecuta comandos en el **host**, sin sandbox | Solo en entorno confiable; `AllowedCommands` restringe los programas; en producción usa `kubernetes.Sandbox` u otro runner aislado vía `backends.Remote` |
| `mcp.Stdio` | Inicia un proceso con tus permisos | Solo servidores confiables; fija versiones; restringe `Env` |
| `backends.Filesystem` | Acceso al disco | Confinado a `Root` (bloquea `..` y symlinks que escapan); define `Permissions` |

### Prompt injection

El contenido que llega de páginas web, documentos, resultados de tools o correos puede contener instrucciones maliciosas. Principios:

1. Da al agent solo las tools que necesita.
2. Exige aprobación humana (`HumanInTheLoop`) para acciones irreversibles o con efecto externo (enviar, pagar, borrar, ejecutar).
3. Restringe destinos y alcances (`messaging.Allow`, SOQL de solo lectura, namespaces de Kubernetes).
4. Aplica los permisos **en la capa de datos** (`spicedb.FilterRetriever`, `permit.FilterRetriever`), sin confiar en que el prompt "no revele".
5. Trata la salida del modelo como entrada no confiable antes de usarla en SQL, shell o HTML.
6. Los documentos recuperados por RAG, CAG o KAG también son entrada no confiable: un fragmento puede contener instrucciones. Filtra por permiso en la recuperación (`Question.Filter`, ítem 4) y nunca pegues texto del modelo en consultas: los tipos de relación y las etiquetas del grafo se validan (`^[A-Z][A-Z0-9_]*$`) antes de llegar a Cypher.

### SSRF

`search.Fetcher` rechaza loopback y redes privadas por defecto, incluso tras resolver el DNS. No actives `AllowPrivate` en código expuesto a entrada de usuarios.

### Secretos

- No registres `Authorization`, claves de API ni el cuerpo de prompts con datos personales en los logs.
- Claves de tracing y de proveedores por entorno. `remote/server` usa una `APIKey` compartida: pon TLS y autenticación real delante.

## 14.5 Rendimiento

- Reutiliza el `http.Client` (`HTTP` en las opciones de proveedores y tracers) para keep-alive.
- `models.WithCache` evita repetir llamadas idénticas (útil en pruebas y evaluaciones).
- `embeddings.NewCached` evita volver a generar embeddings de los mismos textos.
- Usa `Parallel`, ramas paralelas y `Send` para I/O independiente (varias búsquedas, varios documentos).
- Para volumen de vectores, cambia `vectorstores.Memory` (búsqueda lineal) por un backend con índice.
- El streaming (`ModeMessages`) mejora la latencia percibida en chats.

## 14.6 Pruebas

```bash
go test -race ./...                                 # todo; las pruebas de servicios reales se omiten solas (sin red)
go test ./internal/sqlitetest/...                   # sqlsaver contra SQLite real (driver modernc.org/sqlite)
PG_DSN=postgres://... go test ./internal/pgtest/... # PostgreSQL, MariaDB, CockroachDB, Oracle
```

Las pruebas que necesitan servicios reales **se omiten solas** cuando la variable de entorno no está definida (`REDIS_ADDR`, `MONGO_ADDR`, `DYNAMODB_ENDPOINT`, `S3_ENDPOINT`, `SOLR_URL`, `CLICKHOUSE_URL`, `K8S_SERVER`...). Para levantar los servicios, mira [INTEGRATIONS.md](../INTEGRATIONS.md#pruebas-contra-servicios-reales).

Para **tus** pruebas:

- `models.NewFake(...)` con respuestas guionadas; revisa `fake.Calls`.
- `embeddings.Fake{Dim: 64}` para RAG determinístico.
- `httptest.NewServer` para simular proveedores (usa `BaseURL`).
- Prueba el flujo de HITL: invoca, verifica `graph.Interrupts(out)`, reanuda con `graph.Command{Resume: ...}`.

## 14.7 Troubleshooting

| Síntoma | Causa probable | Solución |
|---|---|---|
| `Init` devuelve error de proveedor/clave | Variable `<PROVEEDOR>_API_KEY` ausente o prefijo equivocado | Revisa `chatmodels.Providers()` y las variables del capítulo 1 |
| Error pidiendo `<PROVEEDOR>_BASE_URL` | Proveedor por tenant (Databricks, Snowflake, gateways) | Define la variable con el endpoint |
| `*graph.GraphRecursionError` | El loop superó el límite (predeterminado 25) | Sube `RecursionLimit` si es legítimo; si no, corrige la condición de salida |
| El agent no recuerda la conversación | Sin checkpointer o sin `graph.Thread(id)` en el contexto | `Compile: graph.WithCheckpointer(...)` y `runnables.WithConfig(ctx, graph.Thread(id))` |
| `Interrupt` no pausa / no reanuda | Sin checkpointer, o `thread_id` distinto en la reanudación | Usa el mismo `thread_id`; envía `graph.Command{Resume: ...}` |
| Efecto colateral ejecutado dos veces tras reanudar | El nodo se reejecuta desde el inicio | Mueve la acción después del `Interrupt` o hazla idempotente |
| El estado vuelve como `float64` o `map[string]any` en vez del tipo propio | Serialización JSON del checkpoint | `checkpoint.Register("nombre", Tipo{})` |
| Un número vuelve como `float64` (o `int`) en vez del tipo esperado | Los números sin tipo explícito vuelven como `int` o `float64`; los escalares de Go como `int64` y `float32` ya se conservan | Usa el tipo escalar en el estado, o `checkpoint.Register` para un tipo propio; `checkpoint.Strict(true)` avisa de structs no registradas |
| Las escrituras de ramas paralelas se pierden/chocan | Canal sin reducer | Usa `graph.Append()`, `graph.Sum()` o `channels.BinOp` |
| La tool nunca se llama | Descripción vaga, schema confuso o modelo sin soporte de tools | Mejora el nombre/la descripción/`description` de los campos; prueba con otro modelo |
| La tool se llama con argumentos inválidos | Schema permisivo | Usa `jsonschema:"enum=..."`, `minimum`, `required`; devuelve `tools.Errorf` explicando |
| Respuesta en un solo bloque en Bedrock | Stream binario no decodificado | Esperado: `Stream` devuelve la respuesta completa |
| `human_in_the_loop` no pausa | Sin checkpointer | Pasa `Compile: []graph.CompileOption{graph.WithCheckpointer(s)}` |
| `execute` desaparece en un Deep Agent | Existe una regla `Deny` en `Permissions` | Comportamiento intencional: un shell eludiría las reglas |
| El vector store devuelve vacío | Embedder distinto en la ingestión y en la consulta, o índice aún no creado | Usa el mismo embedder/dimensión; crea el índice antes (Pinecone, Atlas) |
| Los eventos de tracing no aparecen | Falta `Close()` o la cola está llena | `defer tr.Close()`; configura `OnError` y aumenta `Buffer` |
| `mongodb+srv://` no funciona | No soportado | Pasa `host:puerto` |
| Rate limit 429 del proveedor | Cuotas | `models.NewRateLimiter`, `ModelRetry` con backoff, `ModelFallback` |
| El contexto supera el límite del modelo | Historial largo | `Summarization`, `ContextEditing` o `messages.Trim` |

## 14.8 Limitaciones conocidas

- El estado es `map[string]any`, no TypedDict/Pydantic. Usa `tools.Decode` para enlazar en structs.
- En `sqlsaver`, los escalares de Go (`int64`, `float32`, `time.Time`, `[]byte`…) vuelven con su tipo original; los números sin tipo explícito vuelven como `int` o `float64`; los tipos propios requieren `checkpoint.Register`.
- En `graph.Interrupt`, el nodo se reejecuta desde el inicio (como en LangGraph de Python). Los subgrafos propagan los interrupts al padre; cuando varios nodos de un subgrafo se detienen en el mismo super-paso, las pausas aparecen y las respuestas se entregan en el orden de las tasks (sin checkpointer en el subgrafo, ese super-paso se ejecuta en secuencia durante la reanudación).
- No portado de la versión Python: Studio, build/deploy de la CLI, backends gestionados de LangSmith y experimentos alojados. Evaluaciones offline: paquete `evals` ([EVALS.md](../EVALS.md)); servidor de desarrollo: `langgraph/dev` ([DEV.md](../DEV.md)).
- Sin parser de PDF nativo (usa `Unstructured` o convierte antes).
- RAG/CAG/KAG ([capítulo 20](20-rag-cag-kag.md)): la indexación incremental guarda los hashes solo en memoria; el CAG depende de la caché del proveedor (expira y cobra la primera escritura) y estima tokens (~4 caracteres por token); los adapters de grafo puntúan `FindEntities` en Go. Lista completa en [KNOWLEDGE.md](../KNOWLEDGE.md).
- `Flat` y `Annoy` son equivalentes en Go puro a FAISS/Annoy. `LoadAnnoyFile` y `LoadFAISSFlat` leen los formatos nativos (Annoy angular/euclidiano; FAISS flat e IDMap sobre flat), verificados por prueba contra archivos generados por las bibliotecas oficiales (ver [INTEGRATIONS.md](../INTEGRATIONS.md)).
- Las integraciones de SaaS/API de pago solo tienen pruebas de contrato; valídalas en tu entorno.
- 123 proveedores de la lista de LangChain dependen de documentación de API no disponible ([PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md)).

## 14.9 Evolución y compatibilidad

El módulo exige Go 1.27+ y el código de producción no importa dependencias de terceros (los drivers del `go.mod` son solo de prueba), así que actualizar es cambiar la versión en `go.mod` (`go get github.com/fsvxavier/golang-chain-eco@latest`) y ejecutar `go build ./... && go test -race ./...`. Lee el historial del repositorio antes de actualizaciones mayores, porque la API sigue al ecosistema de Python, que evoluciona rápido.

---

Anterior: [13. Integraciones](13-integracoes.md) · Siguiente: [15. Ejemplos y recetas](15-exemplos-e-receitas.md) · [Índice del manual](README.md)
