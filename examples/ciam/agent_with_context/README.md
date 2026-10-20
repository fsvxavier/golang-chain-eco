# agent_with_context

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

An agent that asks the project's context manager before it changes code, and so respects a decision nobody mentioned.

## What it does

- Creates a project with an invoice type and an ADR that says "never use float for money".
- The agent has the `ciam` tools (`kit.All()`) and the ciam instructions as system prompt. Before implementing, it calls `ciam_decision_context` and receives the code **and** the ADR, so it proposes `DiscountCents int64` instead of a float percentage.

## How to apply it

```go
kit := ciam.Toolkit{M: mgr, Project: root}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

The system prompt (`ciam.Instructions`) tells the agent when to consult context. Add the tools to any agent, including deep agents and the Open SWE engineer.

## Run it

```bash
go run ./examples/ciam/agent_with_context
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use a real model and a real indexed project with a real embedder; check the model actually calls `ciam_decision_context` first (look at the tool-call trace).
2. Seed the ADRs that matter (money, auth, data retention) and test prompts that would violate them.
3. Measure tokens per task with and without the tools.
4. Keep the index fresh (re-run `Index` after merges).

## See also

- [Manual, chapter 18: ciam](../../../docs/en-US/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/en-US/CIAM.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
