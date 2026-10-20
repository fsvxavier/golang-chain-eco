# slack_trigger

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Open SWE started from Slack: a signed Events API delivery starts one run on the Slack thread and the outcome is posted back.

## What it does

- Handles Slack's `url_verification` handshake, then an `app_mention` in a thread: it starts one run on the openswe thread derived from the Slack thread.
- Slack's retry of the same event (`X-Slack-Retry-Num`) is ignored; when the run finishes the answer is posted back into the Slack thread. The user's message reaches the model fenced as untrusted (`<untrusted_slack ...>`).
- A fake Slack and GitHub stand in for the real services and a scripted model answers.

## How to apply it

```go
h := &slack.Handler{SigningSecret: secret, BotUserID: botUser, Channels: []string{"C0CODE"}, Dispatcher: d}
http.Handle("/slack/events", h)
runner := slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")), Inner: openswe.AgentRunner{Graph: agent}}
```

Restrict `Channels` to the places where the bot may act. Linear works the same way through `openswe/linear`.

## Run it

```bash
go run ./examples/openswe/slack_trigger
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Create a Slack app (Events API, scopes for mentions and posting) with your real signing secret and bot token (`SLACK_BOT_TOKEN`), and point the request URL at your server over HTTPS.
2. Verify the handshake and that a request with a bad signature or an old timestamp (older than 5 minutes) is rejected.
3. Use durable dispatcher queue and claims so a Slack retry never starts a second run, including across restarts.
4. Mention the bot in a test channel and confirm one reply per mention; confirm mentions outside `Channels` do nothing.
5. Use a real model, real GitHub App and a Kubernetes sandbox as in the `engineer` example.

## See also

- [Manual, chapter 19: Open SWE](../../../docs/en-US/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/en-US/OPENSWE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
