# slack_trigger

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Open SWE iniciado desde Slack: una entrega firmada de la Events API inicia una ejecución en el thread de Slack y el resultado se publica de vuelta.

## Qué hace

- Gestiona el handshake `url_verification` de Slack y luego un `app_mention` en un thread: inicia una ejecución en el thread de openswe derivado del thread de Slack.
- El retry del mismo evento por Slack (`X-Slack-Retry-Num`) se ignora; cuando la ejecución termina, la respuesta se publica de vuelta en el thread de Slack. El mensaje del usuario llega al modelo acotado como no confiable (`<untrusted_slack ...>`).
- Un Slack y un GitHub falsos hacen el papel de los servicios reales y un modelo guionizado responde.

## Cómo aplicarlo

```go
h := &slack.Handler{SigningSecret: secret, BotUserID: botUser, Channels: []string{"C0CODE"}, Dispatcher: d}
http.Handle("/slack/events", h)
runner := slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")), Inner: openswe.AgentRunner{Graph: agent}}
```

Restringe `Channels` a los lugares donde el bot puede actuar. Linear funciona igual mediante `openswe/linear`.

## Cómo ejecutarlo

```bash
go run ./examples/openswe/slack_trigger
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Crea una app de Slack (Events API, scopes para menciones y publicación) con tu signing secret y bot token reales (`SLACK_BOT_TOKEN`) y apunta la request URL a tu servidor por HTTPS.
2. Verifica el handshake y que una petición con firma inválida o timestamp antiguo (más de 5 minutos) se rechaza.
3. Usa cola y claims duraderos en el dispatcher para que un retry de Slack nunca inicie una segunda ejecución, incluso tras reinicios.
4. Menciona al bot en un canal de prueba y confirma una respuesta por mención; confirma que las menciones fuera de `Channels` no hacen nada.
5. Usa un modelo real, una GitHub App real y un sandbox Kubernetes como en el ejemplo `engineer`.

## Ver también

- [Manual, capítulo 19: Open SWE: una fábrica de software con agents](../../../docs/es-ES/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/es-ES/OPENSWE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
