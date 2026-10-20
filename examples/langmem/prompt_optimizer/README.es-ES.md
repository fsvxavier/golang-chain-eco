# prompt_optimizer

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Memoria procedimental: un system prompt que mejora solo a partir de conversaciones y feedback, y un optimizador de varios prompts que decide qué prompt causó un fallo.

## Qué hace

- Una trayectoria (conversación más feedback: el usuario quería código y recibió teoría) va a `langmem.NewPromptOptimizer` con la estrategia `Metaprompt`; refleja y reescribe el prompt.
- `MultiPromptOptimizer` hace asignación de crédito entre varios prompts cooperantes (`research`, `explain`): clasifica cuál es el responsable y actualiza solo ese.

## Cómo aplicarlo

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 2})
better, _ := opt.Optimize(ctx, trajectories, langmem.Prompt{Name: "main", Prompt: current})

mo := &langmem.MultiPromptOptimizer{Model: model, Kind: langmem.PromptMemory}
updated, _ := mo.Optimize(ctx, trajectories, prompts)
```

Recoge trayectorias con notas o comentarios de usuarios o de evals, ejecuta el optimizador offline y **revisa** el nuevo prompt antes de desplegarlo.

## Cómo ejecutarlo

```bash
go run ./examples/langmem/prompt_optimizer
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo fuerte para la optimización y reúne trayectorias reales con feedback real (pulgares, valoraciones, scores de eval).
2. Nunca despliegues automáticamente: evalúa el prompt candidato en un dataset separado (ver `examples/evals/basic`) y compáralo con el actual.
3. Versiona los prompts en control de versiones y guarda la trayectoria que justificó cada cambio.
4. Ejecútalo periódicamente, no por petición; la optimización es un job por lotes.

## Ver también

- [Manual, capítulo 16: Memoria a largo plazo (LangMem)](../../../docs/es-ES/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/es-ES/LANGMEM.md)
- [EVALS.md](../../../docs/es-ES/EVALS.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
