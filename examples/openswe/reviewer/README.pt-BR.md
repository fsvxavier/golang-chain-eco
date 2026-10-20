# reviewer

[English](README.md) | **Português** | [Español](README.es-ES.md)

O revisor de pull requests do Open SWE: somente-leitura, uma review por rodada e uma re-revisão que reconcilia achados anteriores em vez de recomeçar.

## O que faz

- Rodada 1: o revisor busca o diff, registra um achado de severidade alta ancorado na linha (um token escrito no log), tenta `git push` (recusado: reviews são somente-leitura) e publica **uma** review com um comentário inline.
- O autor envia uma correção e responde na thread. A rodada 2 (uma re-revisão) vê o achado antigo e a resposta humana (cercada como não confiável), resolve o achado com uma nota e não publica uma review vazia.
- Os serviços do GitHub (e do Slack) são fakes em processo de `examples/internal` (`fakegithub`, `fakeslack`), então nada sai da sua máquina.

## Como aplicar

```go
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles}
rr.Run(ctx, triggers.Task{Kind: triggers.KindReviewer, Owner: "acme", Repo: "api", Number: 5})
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, CustomPrompt: "Secrets must never reach logs.", Approval: review.ApprovalDryRun})
```

O `Style` por repositório define a habilitação, um prompt customizado e o modo de aprovação (`ApprovalDryRun` registra o veredito sem aprovar no GitHub).

## Como rodar

```bash
go run ./examples/openswe/reviewer
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Comece com `ApprovalDryRun` num repositório real: compare os vereditos do revisor com as reviews humanas por algumas semanas antes de permitir aprovações.
2. Use um modelo real e um GitHub App; mantenha o sandbox somente-leitura (o provider Kubernetes precisa de um pod customizado com `readOnlyRootFilesystem`; veja OPENSWE.md).
3. Persista achados e estilos num `store.Store` durável; os achados se ligam aos ids de comentário do GitHub para que uma re-revisão consiga resolvê-los.
4. Acompanhe a precisão: quantos achados as pessoas marcam como úteis; ajuste o `CustomPrompt` por repositório.
5. Trate o conteúdo do PR e as respostas como entrada não confiável (o revisor já as cerca) e nunca dê credenciais de escrita ao sandbox de review.

## Veja também

- [Manual, capítulo 19: Open SWE: uma fábrica de software com agents](../../../docs/pt-BR/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/pt-BR/OPENSWE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
