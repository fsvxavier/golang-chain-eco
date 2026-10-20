# reviewer

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El revisor de pull requests de Open SWE: de solo lectura, una review por ronda y una re-revisión que reconcilia hallazgos anteriores en lugar de empezar de cero.

## Qué hace

- Ronda 1: el revisor trae el diff, registra un hallazgo de severidad alta anclado a la línea (un token escrito en el log), intenta `git push` (rechazado: las reviews son de solo lectura) y publica **una** review con un comentario inline.
- El autor sube una corrección y responde en el thread. La ronda 2 (una re-revisión) ve el hallazgo antiguo y la respuesta humana (acotada como no confiable), resuelve el hallazgo con una nota y no publica una review vacía.
- Los servicios de GitHub (y de Slack) son falsos en proceso de `examples/internal` (`fakegithub`, `fakeslack`), así que nada sale de tu máquina.

## Cómo aplicarlo

```go
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles}
rr.Run(ctx, triggers.Task{Kind: triggers.KindReviewer, Owner: "acme", Repo: "api", Number: 5})
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, CustomPrompt: "Secrets must never reach logs.", Approval: review.ApprovalDryRun})
```

El `Style` por repositorio define la habilitación, un prompt personalizado y el modo de aprobación (`ApprovalDryRun` registra el veredicto sin aprobar en GitHub).

## Cómo ejecutarlo

```bash
go run ./examples/openswe/reviewer
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Empieza con `ApprovalDryRun` en un repositorio real: compara los veredictos del revisor con las reviews humanas durante unas semanas antes de permitir aprobaciones.
2. Usa un modelo real y una GitHub App; mantén el sandbox de solo lectura (el provider Kubernetes necesita un pod personalizado con `readOnlyRootFilesystem`; ver OPENSWE.md).
3. Persiste hallazgos y estilos en un `store.Store` duradero; los hallazgos se enlazan a los ids de comentario de GitHub para que una re-revisión pueda resolverlos.
4. Sigue la precisión: cuántos hallazgos marcan las personas como útiles; ajusta el `CustomPrompt` por repositorio.
5. Trata el contenido del PR y las respuestas como entrada no confiable (el revisor ya las acota) y nunca des credenciales de escritura al sandbox de review.

## Ver también

- [Manual, capítulo 19: Open SWE: una fábrica de software con agents](../../../docs/es-ES/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/es-ES/OPENSWE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
