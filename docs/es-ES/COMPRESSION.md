# Compresión de llamadas: RTK, Caveman, Ponytail, TOON y un pequeño modelo de decisión

[English](../en-US/COMPRESSION.md) | [Português](../pt-BR/COMPRESSION.md) | **Español**

Referencia del paquete `compress`, del middleware `Compression` en `agents/middleware` y del hook `backends.LocalShell.Rewrite`.
El código de la biblioteca usa solo la biblioteca estándar. El `go.mod` raíz requiere laya-go para el decider nativo de Laya, pero solo la build tag `laya_native` lo compila. `examples/langgraph/decider_laya_native` es un módulo Go separado.

## La idea

Los agents gastan la mayor parte de los tokens en dos cosas: historiales largos de conversación y salida ruidosa de shell. La compresión se decide por llamada, a partir de un resumen pequeño, por un decider:

- **Caveman** acorta el texto de los turnos antiguos y pide una respuesta breve.
- **Ponytail** es una directiva de prompt que hace que un agente de código escriba menos código. Es un texto de reglas, no un compresor.
- **TOON** recodifica los resultados JSON de herramientas en una notación más compacta, solo cuando el resultado queda estrictamente más corto.
- **RTK** (la CLI `rtk`, opcional) acorta la salida de comandos de shell simples.
- **Un decider** responde preguntas tipadas sobre el estado actual. Puede ser un conjunto de reglas (`RuleDecider`) o un modelo System 1: Laya (`HTTPDecider` o el módulo nativo) o, más adelante, TypeSafe JEV.

Un modelo nunca lee el historial completo. Lee un `Summary` (conteos, tamaños, proporción de salida de herramientas, idioma, una cola corta) que cabe en su ventana. La compresión nunca reescribe el estado guardado ni los checkpoints: cambia solo la copia por llamada enviada al modelo, así que un thread reanudado ve el texto original.

## Las preguntas

| Id | Dónde | Tipo | Efecto con "sí" (o el nivel elegido) |
|---|---|---|---|
| `compress_history` | antes de una llamada al modelo | Noul | comprime los turnos antiguos con Caveman |
| `level` | antes de una llamada al modelo (se pregunta cuando historial, respuesta o Ponytail están activos) | Choice: `lite`, `full`, `ultra` | nivel de Caveman y de la directiva Ponytail, compartido por historial y respuesta |
| `compress_reply` | antes de una llamada al modelo | Noul | añade la directiva de respuesta breve tras el último mensaje del usuario |
| `ponytail` | antes de una llamada al modelo | Noul | añade la directiva Ponytail tras el último mensaje del usuario |
| `toon_tool_results` | antes de una llamada al modelo | Noul | reescribe los resultados JSON de herramientas en TOON cuando son estrictamente más cortos |
| `route_rtk` | antes de un comando de shell | Noul | antepone `rtk ` al comando |

Las cinco primeras forman el conjunto de llamadas al modelo (`compress.HistoryQuestions()`, en ese orden). `route_rtk` se pregunta antes de un comando de shell, no antes de una llamada al modelo.

Los ids de pregunta y respuesta son cadenas exactas (`compress.QCompressHistory`, `QLevel`, `QCompressReply`, `QPonytail`, `QTOON`, `QRouteRTK`). Las respuestas se emparejan por id. Las respuestas Noul traen un valor booleano y una probabilidad; las respuestas Choice traen una elección y una distribución.

## Presupuesto del Summary

`compress.Summarize(messages, SummaryOptions{})` construye un `Summary`: conteo de mensajes y turnos, tokens estimados, proporción de salida de herramientas, tamaño del mayor resultado de herramienta, idioma dominante (inglés, portugués, español u otro, por conteo de stopwords) y los últimos mensajes, truncados. Dos señales alimentan las preguntas Ponytail y TOON: `CodeShare` (la proporción de bytes dentro de bloques de código delimitados en los mensajes Human y AI, de 0 a 1; clave de estado `code_share`) y `JSONToolBytes` (el total de bytes de los mensajes de herramienta cuyo contenido es un objeto o array JSON; clave de estado `json_tool_bytes`). Los tokens se estiman como bytes / 4.

Con el `SummaryOptions` por defecto, el estado codificado (`Summary.State()` como JSON) queda por debajo de **700 tokens**, para que las preguntas quepan en una ventana de 1024 tokens del decider. Un mensaje enorme, texto con unicode y un historial vacío quedan todos por debajo del presupuesto (probado). Los `SummaryOptions` mayores no se recortan, así que pueden superarlo.

## Decider y fallback

`compress.Decider` tiene un método: `Decide(ctx, State, []Question) ([]Answer, error)`.

`RuleDecider` no necesita modelo. Sus valores por defecto:

- `compress_history`: sí cuando el historial supera **4000** tokens estimados.
- `level`: `full`.
- `ponytail`: sí cuando `code_share >= 0.1`.
- `toon_tool_results`: sí cuando `json_tool_bytes >= 2048`.
- `route_rtk`: sí cuando la primera palabra es una de `git go ls grep find docker kubectl npm cargo pytest` y la salida anterior de ese comando superó **2048** bytes o es desconocida.

`compress.NewGuarded(inner)` envuelve un decider con estos valores: fallback `RuleDecider{}`, **umbral de confianza 0.7**, **timeout del decider 2s**. Pregunta por pregunta, la respuesta del interno solo se usa cuando existe, tiene el id y el tipo correctos y es confiable. La confianza es la probabilidad de la clase superior en Choice y `max(P, 1-P)` en Noul. Cualquier otra respuesta, error del interno, timeout o panic envía esa pregunta al fallback. La cancelación del contexto de quien llama se devuelve como error. Un decider nunca hace fallar una ejecución.

Las probabilidades de un modelo no están calibradas. El umbral es un ajuste que hay que afinar con datos reales.

## Caveman (compresión de texto)

`compress.Compress(text, lvl)` trabaja sobre la prosa de los turnos antiguos. Los tramos protegidos salen byte a byte iguales: bloques de código delimitados, código en línea, URLs, rutas, cadenas entre comillas, líneas con forma de JSON y código con sangría. También quedan intactas las líneas completas que son filas de tabla Markdown (primer carácter no blanco `|`) o que parecen código: contienen `==`, `!=`, `:=`, `=>`, `->`, `&&`, `||`, `{` o `}`, o terminan en `;` o `) {`.

- **Reglas mecánicas, cualquier idioma, todo nivel:** normaliza CRLF a LF, colapsa secuencias de espacios y líneas en blanco, quita espacios al final de línea, elimina líneas duplicadas consecutivas, colapsa `!` y `?` repetidos.
- **Tablas de palabras** (`Lexicon`) para `en`, `pt`, `es`, `fr`, `de` e `it`: `lite` además quita muletillas; `full` además quita artículos y atenuantes; `ultra` además quita conectores iniciales de frase.
- **Detección de idioma** por llamada, contando stopwords en las tablas registradas. Sin ganador claro, o texto de menos de 5 palabras, solo se aplican las reglas mecánicas. Un idioma sin tabla (por ejemplo japonés o árabe) recibe solo compresión de espacios y repeticiones.
- **`compress.RegisterLexicon(l)`** añade o reemplaza una tabla (seguro para uso concurrente).
- La salida nunca es más larga que la entrada, nunca queda vacía, y la compresión es idempotente.

`compress.ReplyDirective` es la nota de una línea para respuesta breve: `Reply tersely: no filler, no pleasantries, keep code and numbers exact.`

Caveman nunca toca resultados de herramientas, argumentos de llamadas a herramientas, prompts de sistema, JSON, código, rutas, URLs, números ni errores entre comillas.

## RTK (salida de shell)

`compress.NewRTK(decider)` decide antes de ejecutar, a partir del texto del comando y del tamaño de la salida anterior del mismo comando. Nunca ejecuta un comando dos veces para medirlo.

- Solo los comandos **simples** son candidatos. Cualquier comando con pipe, `&&`, `||`, `;`, redirección, `$(...)`, comillas invertidas, salto de línea o `VAR=` al inicio se deja tal cual.
- `rtk` se busca con el `LookPath` sensible al entorno. `rtk` ausente significa pasar sin cambios.
- La lista `AllowedCommands` de `LocalShell` se comprueba sobre el comando **original**, antes de cualquier reescritura.
- `LocalShell.Rewrite` se aplica después de esa comprobación, al comando que se ejecuta. Nil mantiene el comportamiento anterior.
- RTK conserva el código de salida, y las líneas de error de `go vet` sobreviven (probado).

## Ponytail (menos código escrito)

`compress.PonytailDirective(lvl)` devuelve un texto de reglas para un agente de código. La idea viene del proyecto Ponytail (`github.com/DietrichGebert/ponytail`, MIT). La redacción aquí es una reescritura original y más corta, no una copia.

- **Niveles.** `Lite` son dos frases. `Full` (el por defecto) es una escalera de siete peldaños: ¿necesita existir?, ¿ya lo tiene el código?, ¿lo resuelve la biblioteca estándar?, ¿lo cubre una función nativa de la plataforma?, ¿lo resuelve una dependencia instalada?, ¿cabe en una línea?, y solo entonces el código mínimo. `Ultra` es el conjunto más largo y añade "preguntar si debe existir en absoluto". Cada texto ocupa menos de 1200 bytes y termina con una línea `Ponytail:`.
- **La regla que mantiene la seguridad.** El texto dice "perezoso con la solución, nunca con la lectura": el agente debe leer el código afectado de principio a fin antes de elegir un peldaño. Nunca indica al modelo que salte la validación de entrada en fronteras de confianza, el tratamiento de errores que evita pérdida de datos, las comprobaciones de seguridad, lo básico de accesibilidad, o el único test que protege una lógica arriesgada.
- **Dónde va.** `middleware.Compression` añade `"\n\n" + PonytailDirective(level)` al último mensaje Human de la copia por llamada, una vez por llamada, antes de la directiva de respuesta cuando ambas aplican. El prompt de sistema no cambia, así que los prefijos estables en bytes mantienen su caché.
- **Cuándo se pregunta.** La pregunta `ponytail` es sí cuando `CodeShare >= 0.1`, así que solo las conversaciones con una proporción real de código reciben la nota.

Qué se sabe y qué no:

- El proveedor informa **código -53% y tokens -45%** en su propia prueba de 39 tareas. Esos son los números del proveedor. No se reproducen aquí, y este proyecto no los ha medido.
- La directiva es un empujón. Que un modelo escriba menos código depende del modelo y de la tarea. Mídalo con un modelo real (vea el ejemplo más abajo); el ejemplo offline muestra solo lo que recibe el modelo.

## TOON (resultados JSON de herramientas)

TOON (Token-Oriented Object Notation) es una codificación compacta, basada en sangría, de JSON. Este paquete sigue la **especificación v4.4** (`https://github.com/toon-format/spec`, `SPEC.md`) para el subconjunto que implementa.

- `compress.TOONFromJSON(text, compress.TOONOptions{Delimiter})` codifica texto JSON. El delimitador es `","` (por defecto), `"\t"` o `"|"`; cualquier otro valor es error. JSON inválido es error.
- `compress.TOONShorter(text)` devuelve el texto TOON y `true` solo cuando `text` es un objeto o array JSON **y** el TOON es estrictamente más corto. Si no, devuelve la entrada y `false`. Úselo cuando la salida nunca debe ser más larga que la entrada.
- Implementado: sangría de 2 espacios, LF, sin salto de línea final; claves sin comillas solo cuando cumplen `^[A-Za-z_][A-Za-z0-9_.]*$`; cadenas entre comillas cuando están vacías, parecen número, son `true`/`false`/`null`, tienen espacios en los extremos, contienen `: " \ [ ] { }`, un carácter de control o el delimitador activo, o empiezan por `-` o `#`; orden de las claves conservado como en el texto JSON; números en decimal simple con los dígitos más cortos que hacen el viaje de ida y vuelta (forma exponencial fuera de `1e-6 <= |n| < 1e21`), `-0` como `0`, NaN e Inf como `null`, enteros grandes conservados dígito a dígito.
- Arrays: arrays de primitivos en línea (`tags[3]: admin,ops,dev`); arrays de objetos con las mismas claves y solo valores primitivos como bloque **tabular** (`users[2]{id,name,role}:` con una fila por objeto); arrays mixtos o no uniformes en forma de lista, con elementos `- `; arrays vacíos como `key: []`.
- **No implementado:** no hay **decodificador** (TOON de vuelta a JSON), ni la forma **tabular con claves**, ni **key folding**. Los objetos anidados se escriben como sangría anidada. La pérdida cero depende de los tests golden en `compress/toon_test.go`, que usan los ejemplos de la especificación, no un ida y vuelta.

**En el middleware.** La pregunta `toon_tool_results` es sí cuando `JSONToolBytes >= 2048`. Entonces cada mensaje de herramienta cuyo contenido es un objeto o array JSON, y que no tiene `Parts`, se sustituye en la copia por llamada por su forma TOON, pero solo cuando esa forma es estrictamente más corta. Los ids de las llamadas a herramientas y el orden de los mensajes nunca cambian. La salida de herramientas que no es JSON y los mensajes con `Parts` quedan intactos. El estado guardado no se reescribe.

Esta es la única excepción a "los resultados de herramientas nunca se tocan". Caveman sigue sin tocarlos.

**El ahorro depende del modelo.** TOON solo ayuda si el modelo lo lee bien, y el ahorro de tokens del modelo varía con el tokenizador. El proyecto TOON publica sus propios benchmarks; no se reproducen aquí. El ejemplo informa bytes y una estimación (`messages.ApproxTokens`, bytes / 4), no tokens facturados.

## Middleware de compresión

`middleware.NewCompression(decider)` (en `agents/middleware`) es una transformación `WrapModelCall`:

- Construye el `Summary`, pregunta al decider protegido por guard, y aplica Caveman a los turnos antiguos de Human y AI, la directiva Ponytail (cuando se pregunta) y la directiva de respuesta tras el último mensaje Human, y TOON a los resultados JSON de herramientas (cuando se pregunta).
- `Keep` es el número de turnos recientes que no se tocan (por defecto **4**). El umbral de confianza (**0.7**) se define en `compress.Guarded`, no en `Compression`: `NewCompression` envuelve el decider con `NewGuarded`. Para cambiarlo, construya un `compress.Guarded{...}` y páselo como `Compression{Decider: g}`.
- Las llamadas a herramientas nunca se tocan. Los resultados de herramientas cambian solo por la regla TOON de arriba, así que cada id de llamada y el orden de los pares se mantienen.
- Solo cambia la copia por llamada. El estado guardado y los checkpoints conservan el texto original.
- La directiva de respuesta va tras el último mensaje del usuario, no en el prompt de sistema, para que los prefijos estables en bytes (`cag`) mantengan su caché.
- `Summarization` sigue siendo un middleware `BeforeModel` separado, no un paso dentro de Compression (el orden de los hooks hace imposible "solo si sigue por encima tras Caveman"). Fije su `MaxTokens` por encima del umbral del decider para que actúe como **respaldo** en historiales que Caveman no reduce lo suficiente.

## HTTPDecider: servidor Laya

`compress.HTTPDecider{URL, APIKey, Model, Client}` consulta un servidor Laya por HTTP con `net/http`, sin código de terceros. URL por defecto: `http://localhost:8000/v1/systemone`.

El servidor oficial es `laya-serve`, un paquete de Python:

```bash
pip install "laya[serve]"
LAYA_MODELS=... LAYA_DEVICE=cpu LAYA_HOST=127.0.0.1 LAYA_PORT=8000 LAYA_API_KEY=... laya-serve
```

Variables de entorno: `LAYA_MODELS` (modelos a cargar), `LAYA_DEVICE`, `LAYA_HOST`, `LAYA_PORT`, `LAYA_API_KEY` (se envía como clave bearer). La forma `laya serve --model` no aparece en la documentación pública y no se usa aquí.

**Privacidad:** la petición lleva el estado del `Summary`, incluida la cola: los últimos 3 mensajes, cada uno recortado a 160 bytes de texto codificado en JSON, con la salida de las herramientas. La cola va a la URL del decider, así que use un endpoint de confianza. El middleware siempre usa el `SummaryOptions` por defecto, y `TailMessages` 0 significa el valor por defecto 3, no ninguno. Para mantener la cola en su máquina, use un decider local (`RuleDecider`, o el decider nativo laya-go), no `HTTPDecider`.

El contrato es `POST /v1/systemone`: un cuerpo JSON con el estado y las preguntas tipadas, y una respuesta JSON con las respuestas tipadas. Es el mismo protocolo que el de TypeSafe JEV, así que un endpoint JEV debería funcionar con URL y clave. **Eso no está probado.**

**Punto abierto:** la forma de la respuesta Noul de `laya-serve` (booleano con o sin probabilidad) no está documentada aquí. Un booleano sin confianza cae en las reglas a través del guard. Revise una respuesta real antes de depender de las probabilidades de Laya.

El ejemplo `decider_laya_http` funciona offline contra un servidor falso en proceso. Con `LAYA_URL` (y `LAYA_API_KEY`, `LAYA_MODEL`) usa un servidor real.

## Laya nativo (laya-go)

`github.com/metalagman/laya-go` v0.2.6 ejecuta el modelo en proceso. Necesita cgo y ONNX Runtime. El `go.mod` raíz lo requiere, pero solo se compila con la build tag `laya_native`, en `examples/langgraph/full_team/native`, un paquete del módulo raíz. Calificación: **linux/amd64, solo FP32**. Una entrada más larga que la ventana del modelo devuelve `ErrInputTooLong`, salvo que el truncamiento esté activado.

Ejemplo: `examples/langgraph/decider_laya_native` es un módulo Go separado (con `go.mod` propio y `replace` a la raíz del repositorio). Sin la tag o sin `LAYA_BUNDLE_DIR`, muestra el motivo y usa `RuleDecider`. Sin la tag, `go build ./...` no compila laya-go y no necesita cgo.

## JEV (planificado)

TypeSafe JEV es el segundo modelo System 1. Aquí figura como planificado y **no construido**: no hay carpeta `decider_jev` ni código. La página pública de JEV no declara API, soporte para Go ni tamaño de ventana. El cliente HTTP de arriba es el camino a probar primero.

## Ejemplos

Todos se ejecutan offline con un modelo falso, salvo que se configure un servicio real. Cada uno tiene `README.md`, `README.pt-BR.md` y `README.es-ES.md`.

| Ejemplo | Muestra |
|---|---|
| `langgraph/decider_fallback` | la interfaz `Decider`, `RuleDecider` y `Guarded`, sin modelo |
| `langgraph/compress_history_summary` | `Summary` de un historial por encima de 8k tokens, cabe en el presupuesto de 700 tokens |
| `langgraph/compress_shell_rtk` | salida de shell por RTK cuando el decider lo decide |
| `langgraph/compress_prompt_caveman` | compresión de prompt y de respuesta con el middleware |
| `langgraph/compress_toon` | un resultado JSON de 20 filas, su forma TOON, tamaños antes y después, y el fallback `TOONShorter` para un objeto pequeño |
| `langgraph/compress_ponytail` | la nota Ponytail que recibe el modelo en cada nivel, y la prueba de que el prompt de sistema no cambia |
| `langgraph/decider_laya_http` | `HTTPDecider` contra `laya-serve` (servidor falso offline) |
| `langgraph/decider_laya_native` | laya-go nativo, módulo separado, tag `laya_native` |
| `langgraph/full_team` | el equipo de `independent_engineering_team` con compresión de llamadas; decider y modelo falsos por defecto. Con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY`, la herramienta de shell ofrece solo `ls`, `grep` y `echo` |

Prueba en modo producción: configure la URL real del decider o el bundle nativo, y ejecute `go test -race -count=1` en el módulo del ejemplo.
