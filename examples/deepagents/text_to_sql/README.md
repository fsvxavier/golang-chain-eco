# text_to_sql

A deep agent that plans, validates a query with a subagent and runs it through a read-only SQL tool.

## What it does

- The lead writes a todo list, notes the schema in `/schema.md`, asks a `query-validator` subagent to review the SQL, then runs `sql_query`.
- `StreamEvents` with `graph.ModeUpdates` prints each tool call and the final answer as they happen.

## How to apply it

Separate "write the query", "check the query" and "run the query" so a safety review sits between generation and execution.

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{sqlQuery},
    SubAgents: []deepagents.SubAgentSpec{{Name: "query-validator", Description: "Reviews SQL for safety.", Model: validator}},
})
```

Pair it with the read-only guard shown in `examples/langgraph/sql_agent`.

## Run it

```bash
go run ./examples/deepagents/text_to_sql
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Point `sql_query` at a read-only replica with a database role that only has `SELECT`; the prompt is not a security boundary, the role is.
2. Reject non-`SELECT` statements, multiple statements and missing `LIMIT` in code before the query reaches the database, and set a statement timeout.
3. Put the schema notes in a skill or memory file so the agent does not rediscover the schema on each question.
4. Build a small set of questions with known results and compare the numbers, not the wording.
5. Log every executed query with the user and thread id for audit.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Manual, chapter 5: Tools](../../../docs/en-US/manual/05-tools.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
