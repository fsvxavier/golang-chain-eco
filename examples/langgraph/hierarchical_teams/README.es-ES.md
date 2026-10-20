# hierarchical_teams

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un supervisor de nivel superior delega a subgrafos de equipos, cada uno con su propio supervisor y workers.

## Qué hace

- Dos equipos (investigación, redacción) son grafos compilados con un supervisor que ejecuta sus workers en orden.
- El supervisor superior enruta a un equipo, el equipo corre como un nodo y devuelve solo los mensajes que añadió; luego el control vuelve al supervisor superior hasta que dice `FINISH`.
- No se usa ningún modelo; los supervisores son reglas para que el foco sea la estructura.

## Cómo aplicarlo

Compón equipos como subgrafos y haz de cada supervisor una llamada a un LLM cuando las decisiones requieran criterio:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := sub.Invoke(ctx, graph.State{"messages": s["messages"]})
    return graph.State{"messages": ms[len(prev):]}, nil // only what the team added
})
```

Devolver solo el delta mantiene el estado del padre libre de duplicados. Para un único nivel mira `multi_agent_supervisor`.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/hierarchical_teams
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Cambia los supervisores basados en reglas por llamadas a modelos restringidas a un conjunto cerrado de rutas; añade límites de bucle en ambos niveles.
2. Usa agents reales (`agents.Create`) como workers, cada uno con sus propias tools.
3. Añade un checkpointer para que las ejecuciones largas se reanuden; haz streaming con `graph.Subgraphs()` para seguir los equipos internos.
4. Haz trace por nombre de equipo (`graph.WithName`) y define presupuestos por equipo.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
