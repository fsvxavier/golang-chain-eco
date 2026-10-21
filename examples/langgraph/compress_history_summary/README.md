# compress_history_summary

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Builds a long history (40 turns with tool results, about 13k estimated tokens) and shows the bounded `compress.Summary` that deciders read in place of the transcript.

## What it does

- `compress.Summarize(msgs, compress.SummaryOptions{})` counts messages, turns, tokens, the largest tool result and the share of tool bytes, detects the language, and keeps the last 3 messages clipped to 160 bytes each.
- `Summary.EncodedTokens()` measures the JSON of `Summary.State()` at 4 bytes per token. It must stay at or below the 700-token budget. The example exits with an error if it does not.
- The example prints the message count, the history tokens, the summary tokens and the summary JSON.

## How to apply it

```go
s := compress.Summarize(msgs, compress.SummaryOptions{})
answers, err := compress.NewGuarded(modelDecider).Decide(ctx, s.State(), compress.HistoryQuestions())
```

Send the Summary to the decider, not the history. Its size does not grow with the history: a 5 MB message costs the same as a short one, because only a bounded prefix of each message is read.

## Run it

```bash
go run ./examples/langgraph/compress_history_summary
```

Expected output lines: `messages: 120`, `history tokens: 13160` (or near), `summary tokens: 130 (budget 700)`, then the `summary:` JSON with `messages`, `turns`, `tokens`, `tool_share`, `largest_tool_bytes`, `code_share`, `json_tool_bytes`, `lang` and `tail`.

## Test it in production mode

1. Feed a real transcript and check that `summary tokens` stays under the budget on your largest sessions.
2. Check `lang` on mixed-language histories. The detector only samples 4 KiB per message.
3. Keep the summary's `tail` short. It carries the most recent turns, so a wrong tail is a wrong decision.
4. Run `go test -race -count=1 ./compress` after changing the budget or the options.

## See also

- [Decider fallback example](../decider_fallback/README.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
