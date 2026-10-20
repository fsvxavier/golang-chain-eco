# chatbot_simulation

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A simulated user talks to your assistant, and an evaluator scores the conversation.

## What it does

- Three roles in one graph: `assistant`, `simulated_user` (a model that plays the customer, with roles flipped) and `evaluate` (a grader returning JSON with a score).
- The loop ends when the simulated user says `FINISHED` or the conversation gets long.
- Prints each turn and the final verdict.

## How to apply it

Use it as a regression harness for a chatbot: write personas and goals for the simulated user, run many conversations and compare scores before and after a prompt or model change.

```go
out, _ := grader.Generate(ctx, []messages.Message{messages.Human("Score this support chat 1-5 as JSON:\n" + messages.Buffer(ms))})
```

## Run it

```bash
go run ./examples/langgraph/chatbot_simulation
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Replace the assistant with your real agent (the same one you deploy) and use real models for the user and the grader.
2. Build 20+ scenarios (happy path, angry customer, off-topic, adversarial) and run each several times: simulations are stochastic.
3. Make the grader return structured criteria, not one number, and spot-check its scores against human ratings.
4. Run it in CI on every prompt or model change and fail the build on a score drop; see also `examples/evals/basic`.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 12: Observability](../../../docs/en-US/manual/12-observabilidade.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
