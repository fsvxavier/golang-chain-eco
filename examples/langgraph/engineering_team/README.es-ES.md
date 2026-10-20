# engineering_team

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Una organización de ingeniería como un único grafo: un gerente enruta la solicitud, un product owner la especifica, un arquitecto evalúa el riesgo, un tech leader la divide en tareas y cuatro ingenieros construyen en paralelo; cada tarea pasa por code review y QA antes de la entrega. Todos los agents corren en un solo proceso.

## Qué hace

El reparto, cada uno con su persona en `skills/<nombre>/SKILL.md`:

| Rol | Cant. | Función |
|---|---|---|
| Software Engineer Manager | 1 | Recibe la solicitud, la enruta y escribe el informe final. Nunca escribe código (el nodo falla si la respuesta contiene un bloque de código). |
| Product Owner | 1 | Convierte la solicitud en una spec con criterios de aceptación numerados. |
| Arquitecto | 1 | Califica el riesgo y define restricciones y reglas de seguridad que acompañan a cada tarea. |
| Tech Leader | 1 | Divide la spec en tareas y califica cada una con complejidad `low`, `medium` o `high`. |
| Software Engineers | 4 | 2 junior, 1 semi senior y 1 senior; implementan en paralelo. |
| Revisores especialistas | 2 | Aprueban una tarea o la devuelven con feedback. |
| Ingenieros de QA | 2 | Aprueban una tarea o la devuelven con un defecto reproducible. |

Flujo: `manager -> po -> architect -> leader -> (dev -> review -> qa -> done) x N -> deliver`. Una tarea devuelta por la review o por QA vuelve al mismo ingeniero con el feedback; tras 3 devoluciones se reporta como `blocked`.

Tres mecanismos lo hacen funcionar:

- **Reserva atómica.** El registro reserva personas bajo un único lock, así que dos tareas nunca reciben al mismo ingeniero, revisor o QA. La asignación del líder prefiere el encaje por complejidad (`high` nunca va a un junior); cuando nadie adecuado está libre, la tarea espera en un backlog y se asigna cuando alguien queda libre. El plan de la demo tiene cinco tareas para cuatro ingenieros.
- **El contexto viaja con el trabajo.** Cada tarea lleva el dossier completo (solicitud, spec, evaluación del arquitecto) y el feedback recibido, porque el argumento de un `Send` es todo lo que ve un nodo. El estado compartido guarda el historial de handoffs en un thread con checkpoint (`graph.Thread("coupons")`).
- **Un nodo `deliver` diferido** corre una vez que todas las tareas salieron del pipeline, y luego el gerente escribe el informe.

## Cómo aplicarlo

- Edita las personas: cada `skills/<nombre>/SKILL.md` es el system prompt de su agent (el código Go embebe la carpeta con `go:embed` y quita el frontmatter). Cada skill define el formato de salida que el código interpreta (`TASK|id|título|complejidad|descripción`, `VERDICT: APPROVE|RETURN`, `RISK:`, ...); cambia los formatos en la skill y en `main.go` a la vez.
- Cambia el equipo: añade miembros en `newRegistry` y una skill para cualquier rol nuevo.
- Cambia el pipeline: los nodos son funciones simples en `main.go`; el enrutamiento es `Command{Send}`.
- El registro es un mutex local al proceso. Para agents en procesos separados haría falta un store con compare-and-swap o lease, más una cola duradera (ver `remote/` y `agentprotocol/`).

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/engineering_team
go test -race ./examples/langgraph/engineering_team   # pruebas de atomicidad del registro y del backlog
```

Offline, un modelo guionizado (`brain.go`) responde según la línea `ROLE:` de cada prompt y hace que la review devuelva T3 y QA devuelva T2 una vez, para que el regreso al ingeniero sea visible. Con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY`, el mismo grafo corre con un modelo real (un modelo para todo el equipo; la persona viene de cada skill). Espera que las líneas paralelas se intercalen distinto en cada ejecución.

## Cómo probarlo en modo productivo

1. Define una clave de proveedor y ejecuta de nuevo. La estructura debe mantenerse (las cinco tareas llegan a `approved` o `blocked`), pero la redacción y el número de rondas de review variarán. Si la ejecución falla por un error de parsing, un modelo ignoró el formato de salida de una skill: ajusta esa skill.
2. Haz la comprobación más estricta que el modelo: el código devuelto por un ingeniero es texto. En producción, haz que el nodo del ingeniero lo escriba en un sandbox y ejecute el build y las pruebas reales antes de la review (ver `examples/integrations/remote_sandbox` y `kubernetes.Sandbox`).
3. Cambia `checkpoint.NewMemory()` por un saver duradero (`sqlsaver`, Redis, ...) y deriva el `thread_id` del id de tu elemento de trabajo; mata el proceso a mitad de una ejecución y reanuda.
4. Acota el coste: el bucle tiene `maxRounds`; define también `ModelCallLimit`/timeouts por agent y un `RecursionLimit` en el grafo.
5. Si los agents deben vivir en servicios separados, pon cada uno detrás de `agentprotocol`/`remote.Graph` y cambia el registro en proceso por un store de reservas compartido.
6. Evalúa las personas como código: ejecuta un conjunto de solicitudes (`examples/evals/basic`) y compara planes, tasa de defectos detectados por la review y defectos de QA antes y después de editar una skill.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
- `examples/langgraph/multi_agent_supervisor` y `hierarchical_teams`: formas de supervisor más simples
