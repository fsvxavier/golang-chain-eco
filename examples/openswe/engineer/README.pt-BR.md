# engineer

[English](README.md) | **Português** | [Español](README.es-ES.md)

O engenheiro do Open SWE de ponta a ponta: edita arquivos no próprio sandbox, é barrado quando tenta contornar a tool de PR e entrega um pull request em rascunho auditado.

## O que faz

- Um engenheiro roteirizado trabalha a issue `acme/api#12` do GitHub: grava `/api/auth/login.go` no sandbox da própria thread, tenta o atalho proibido `gh pr create`, que uma guarda recusa, depois abre o PR com `open_pull_request` e comenta na issue.
- Mostra o que as guardas e o prompt fazem: o `AGENTS.md` do repositório chega ao system prompt, PRs novos são rascunhos, só orgs permitidas são aceitas e o PR é vinculado à thread (`PRLinks`).
- Os serviços do GitHub (e do Slack) são fakes em processo de `examples/internal` (`fakegithub`, `fakeslack`), então nada sai da sua máquina.

## Como aplicar

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager, Links: links, Checkpointer: saver,
    DraftPRs: true, AllowedOrgs: []string{"acme"}, DefaultRepo: "acme/api",
})
err := openswe.AgentRunner{Graph: agent}.Run(ctx, triggers.Task{Kind: triggers.KindAgent, ThreadID: thread, Owner: "acme", Repo: "api", Number: 12, Messages: msgs})
```

Um sandbox por thread, nunca substituído em silêncio: uma substituição estaria vazia e perderia trabalho não publicado. Passe um `*ciam.Toolkit` como `Config.Context` para dar ao engenheiro contexto do projeto.

## Como rodar

```bash
go run ./examples/openswe/engineer
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use um modelo real e um GitHub App real: `github.App{AppID, Key}` (JWT RS256 para um token de instalação, em cache) em vez do fake. Dê ao sandbox um token **sem** permissão de criar PRs, para que `PRCreationGuard` seja mais do que uma barreira de conveniência.
2. Troque `sandbox.Local` (um diretório do host, **sem isolamento**) pelo provider `Kubernetes` (um pod) ou pelo seu próprio `sandbox.Provider`; mantenha `AllowedOrgs` restrito.
3. Use stores duráveis (`sqlstore`, Redis ou Mongo para `store.Store`; um `checkpoint.Saver` durável) para que threads, vínculos de PR e ids de sandbox sobrevivam a reinícios.
4. Mantenha `RunLimit`, `ModelTimeout` e `RecursionLimit` (o engenheiro precisa de um limite de recursão alto por causa dos hooks de middleware).
5. Teste num repositório descartável primeiro: abra uma issue, mencione o bot, revise o PR em rascunho que ele abre e confirme que um comando proibido (`gh pr create`, force push) é recusado.

## Veja também

- [Manual, capítulo 19: Open SWE: uma fábrica de software com agents](../../../docs/pt-BR/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/pt-BR/OPENSWE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
