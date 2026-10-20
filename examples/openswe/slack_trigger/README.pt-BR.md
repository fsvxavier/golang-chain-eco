# slack_trigger

[English](README.md) | **Português** | [Español](README.es-ES.md)

O Open SWE iniciado pelo Slack: uma entrega assinada da Events API inicia uma execução na thread do Slack e o resultado é publicado de volta.

## O que faz

- Trata o handshake `url_verification` do Slack e depois um `app_mention` numa thread: inicia uma execução na thread do openswe derivada da thread do Slack.
- O retry do mesmo evento pelo Slack (`X-Slack-Retry-Num`) é ignorado; quando a execução termina, a resposta é publicada de volta na thread do Slack. A mensagem do usuário chega ao modelo cercada como não confiável (`<untrusted_slack ...>`).
- Um Slack e um GitHub fakes fazem o papel dos serviços reais e um modelo roteirizado responde.

## Como aplicar

```go
h := &slack.Handler{SigningSecret: secret, BotUserID: botUser, Channels: []string{"C0CODE"}, Dispatcher: d}
http.Handle("/slack/events", h)
runner := slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")), Inner: openswe.AgentRunner{Graph: agent}}
```

Restrinja `Channels` aos lugares onde o bot pode agir. O Linear funciona do mesmo jeito por `openswe/linear`.

## Como rodar

```bash
go run ./examples/openswe/slack_trigger
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Crie um app do Slack (Events API, escopos para menções e publicação) com o seu signing secret e bot token reais (`SLACK_BOT_TOKEN`) e aponte a request URL para o seu servidor por HTTPS.
2. Verifique o handshake e que uma requisição com assinatura inválida ou timestamp antigo (mais de 5 minutos) é rejeitada.
3. Use fila e claims duráveis no dispatcher para que um retry do Slack nunca inicie uma segunda execução, inclusive entre reinícios.
4. Mencione o bot num canal de teste e confirme uma resposta por menção; confirme que menções fora de `Channels` não fazem nada.
5. Use um modelo real, um GitHub App real e um sandbox Kubernetes como no exemplo `engineer`.

## Veja também

- [Manual, capítulo 19: Open SWE: uma fábrica de software com agents](../../../docs/pt-BR/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/pt-BR/OPENSWE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
