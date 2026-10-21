# compress_toon

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Encodes a JSON tool result (a 20-row user list) as TOON, then prints the JSON and TOON forms, their byte and estimated-token sizes, and the `TOONShorter` fallback for a tiny array that TOON would not shorten.

## What it does

- Builds 20 deterministic user rows and marshals them to JSON, the way a tool result would arrive.
- `compress.TOONFromJSON(text, compress.TOONOptions{})` encodes the JSON as TOON. The uniform rows become one tabular block: `users[20]{id,name,email,active,team}:` followed by one line per row.
- Compares sizes with `messages.ApproxTokens` (bytes / 4 per message) before and after.
- `compress.TOONShorter` returns the TOON text only when it is strictly shorter. For `[1]` it keeps the JSON unchanged.

The example exits with an error if the TOON form is not shorter, or if `TOONShorter` rewrites the tiny value. It makes no network calls.

## How to apply it

Directly, in your own code:

```go
toon, err := compress.TOONFromJSON(resultJSON, compress.TOONOptions{})
if err == nil && len(toon) < len(resultJSON) {
    msg.Content = toon
}
```

Or let the compression middleware do it per call. Its decider answers the `toon_tool_results` question; when that answer is true, every Tool message with no `Parts` whose content is JSON is replaced by `compress.TOONShorter(content)`. Tool-call ids and message order do not change, and stored state is untouched.

Use `TOONOptions{Delimiter: "|"}` or `"\t"` only when the values contain commas. The delimiter is declared in each array header.

## Run it

```bash
go run ./examples/langgraph/compress_toon
```

Expected output: the JSON document (1716 bytes), the TOON document (902 bytes), `bytes: 1716 -> 902 (saved 814)`, `estimated tokens: 432 -> 228`, and the fallback line `rewritten: false, output: [1]`. The output is the same on every run.

## Test it in production mode

1. Pick a real tool result that returns a list of objects with the same keys. Check its TOON size with `TOONFromJSON`. Nested or irregular results gain less, and mixed arrays fall back to list form.
2. Run a real provider with `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set, once with the middleware's `toon_tool_results` answer forced to `false` and once with it `true`. Compare the input token counts reported by the provider. The estimate here is only bytes / 4.
3. Check that the model still answers correctly from the TOON form. Field values that contain `: " \ [ ] { }`, the delimiter, or leading `-` or `#` are quoted by the encoder, so the model should see them unchanged.
4. Run `go test -race -count=1 ./compress` after changing the encoder.

The TOON format is specified at https://github.com/toon-format/spec (spec v4.4). This package implements the encoder only, without key folding or a decoder.

## See also

- [compress_history_summary](../compress_history_summary/README.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
