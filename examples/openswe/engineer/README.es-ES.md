# engineer

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El ingeniero de Open SWE de punta a punta: edita archivos en su propio sandbox, se le bloquea cuando intenta saltarse la tool de PR y entrega un pull request en borrador auditado.

## Qué hace

- Un ingeniero guionizado trabaja la issue `acme/api#12` de GitHub: escribe `/api/auth/login.go` en el sandbox de su propio thread, prueba el atajo prohibido `gh pr create`, que una guarda rechaza, luego abre el PR con `open_pull_request` y comenta en la issue.
- Muestra qué hacen las guardas y el prompt: el `AGENTS.md` del repositorio llega al system prompt, los PR nuevos son borradores, solo se aceptan orgs permitidas y el PR queda vinculado al thread (`PRLinks`).
- Los servicios de GitHub (y de Slack) son falsos en proceso de `examples/internal` (`fakegithub`, `fakeslack`), así que nada sale de tu máquina.

## Cómo aplicarlo

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager, Links: links, Checkpointer: saver,
    DraftPRs: true, AllowedOrgs: []string{"acme"}, DefaultRepo: "acme/api",
})
err := openswe.AgentRunner{Graph: agent}.Run(ctx, triggers.Task{Kind: triggers.KindAgent, ThreadID: thread, Owner: "acme", Repo: "api", Number: 12, Messages: msgs})
```

Un sandbox por thread, nunca sustituido en silencio: un reemplazo estaría vacío y perdería trabajo no publicado. Pasa un `*ciam.Toolkit` como `Config.Context` para dar al ingeniero contexto del proyecto.

## Cómo ejecutarlo

```bash
go run ./examples/openswe/engineer
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa un modelo real y una GitHub App real: `github.App{AppID, Key}` (JWT RS256 a un token de instalación, cacheado) en lugar del falso. Da al sandbox un token **sin** permiso para crear PR, para que `PRCreationGuard` sea más que una barrera de conveniencia.
2. Cambia `sandbox.Local` (un directorio del host, **sin aislamiento**) por el provider `Kubernetes` (un pod) o por tu propio `sandbox.Provider`; mantén `AllowedOrgs` restringido.
3. Usa stores duraderos (`sqlstore`, Redis o Mongo para `store.Store`; un `checkpoint.Saver` duradero) para que threads, vínculos de PR e ids de sandbox sobrevivan a los reinicios.
4. Mantén `RunLimit`, `ModelTimeout` y `RecursionLimit` (el ingeniero necesita un límite de recursión alto por los hooks de middleware).
5. Prueba primero en un repositorio desechable: abre una issue, menciona al bot, revisa el PR en borrador que abre y confirma que un comando prohibido (`gh pr create`, force push) se rechaza.

## Ver también

- [Manual, capítulo 19: Open SWE: una fábrica de software con agents](../../../docs/es-ES/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/es-ES/OPENSWE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
