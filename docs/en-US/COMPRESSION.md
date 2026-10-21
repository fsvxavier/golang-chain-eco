# Call compression: RTK, Caveman and a small decision model

**English** | [Português](../pt-BR/COMPRESSION.md) | [Español](../es-ES/COMPRESSION.md)

Reference for the `compress` package, the `agents/middleware` `Compression` middleware and the `backends.LocalShell.Rewrite` hook.
The library code uses only the standard library. The root `go.mod` requires laya-go for the native Laya decider, but only the `laya_native` build tag compiles it. `examples/langgraph/decider_laya_native` is a separate Go module.

## The idea

Agents spend most of their tokens on two things: long conversation histories and noisy shell output. Compression is decided per call, from a small summary, by a decider:

- **Caveman** shortens the text of old turns, and asks for a terse reply.
- **RTK** (the `rtk` CLI, optional) shortens the output of simple shell commands.
- **A decider** answers typed questions about the current state. It is a rule set (`RuleDecider`), or a System 1 model: Laya (`HTTPDecider` or the native module), or TypeSafe JEV later.

A model never reads the whole history. It reads a `Summary` (counts, sizes, tool share, language, a short tail) that fits its window. Compression never rewrites stored state or checkpoints: it changes only the per-call copy sent to the model, so a resumed thread sees the original text.

## The four questions

| Id | Site | Kind | Effect when "yes" (or the chosen level) |
|---|---|---|---|
| `compress_history` | before a model call | Noul | compress old turns with Caveman |
| `level` | before a model call (asked when history or reply is on) | Choice: `lite`, `full`, `ultra` | Caveman level, shared by history and reply |
| `compress_reply` | before a model call | Noul | append the terse-reply directive after the last user message |
| `route_rtk` | before a shell command | Noul | prefix the command with `rtk ` |

Question and answer ids are exact strings (`compress.QCompressHistory`, `QLevel`, `QCompressReply`, `QRouteRTK`). Answers are matched by id. Noul answers carry a truth value and a probability; Choice answers carry a pick and a distribution.

## Summary budget

`compress.Summarize(messages, SummaryOptions{})` builds a `Summary`: message and turn counts, estimated tokens, the share of tool output, the size of the largest tool result, the dominant language (English, Portuguese, Spanish or other, by stopword count) and the last messages, truncated. Tokens are estimated as bytes / 4.

With the default `SummaryOptions`, the encoded state (`Summary.State()` as JSON) stays under **700 tokens**, so the questions fit a 1024-token decider window. A huge message, unicode text and an empty history all stay under the budget (tested). Larger `SummaryOptions` are not clipped, so they can exceed it.

## Decider and fallback

`compress.Decider` is one method: `Decide(ctx, State, []Question) ([]Answer, error)`.

`RuleDecider` needs no model. Its defaults:

- `compress_history`: yes when the history is above an estimated **4000** tokens.
- `level`: `full`.
- `route_rtk`: yes when the first word is one of `git go ls grep find docker kubectl npm cargo pytest` and the previous output of that command was over **2048** bytes or unknown.

`compress.NewGuarded(inner)` wraps a decider with these defaults: fallback `RuleDecider{}`, confidence **floor 0.7**, decider **timeout 2s**. Per question, the inner answer is used only when it exists, has the right id and kind, and is confident. Confidence is the top-class probability for Choice and `max(P, 1-P)` for Noul. Any other answer, an inner error, a timeout or a panic sends that question to the fallback. Cancellation of the caller's context is returned as an error. A decider never fails a run.

Probabilities from a model are not calibrated. The floor is a setting to tune with real data.

## Caveman (text compression)

`compress.Compress(text, lvl)` works on the prose of old turns. Protected spans come out byte-identical: fenced code blocks, inline code, URLs, paths, quoted strings, JSON-looking lines and indented code. Whole lines are also left alone when they are Markdown table rows (first non-space character `|`) or look like code: they contain `==`, `!=`, `:=`, `=>`, `->`, `&&`, `||`, `{` or `}`, or end with `;` or `) {`.

- **Mechanical rules, every language, every level:** normalize CRLF to LF, collapse runs of spaces and blank lines, trim trailing spaces, drop consecutive duplicate lines, collapse repeated `!` and `?`.
- **Word tables** (`Lexicon`) for `en`, `pt`, `es`, `fr`, `de` and `it`: `lite` also drops fillers; `full` also drops articles and hedges; `ultra` also drops sentence-initial connectors.
- **Language detection** per call, by counting stopword hits across the registered lexicons. No clear winner, or text under 5 words, means mechanical rules only. A language with no table (for example Japanese or Arabic) gets whitespace and repetition compression only.
- **`compress.RegisterLexicon(l)`** adds or replaces a table (safe for concurrent use).
- Output is never longer than the input, and never empty. Compression is idempotent.

`compress.ReplyDirective` is the one-line terse-reply note: `Reply tersely: no filler, no pleasantries, keep code and numbers exact.`

Caveman never touches tool results, tool-call arguments, system prompts, JSON, code, paths, URLs, numbers or quoted errors.

## RTK (shell output)

`compress.NewRTK(decider)` decides before running, from the command text and the size of the previous output of the same command. It never runs a command twice to measure.

- Only **simple** commands are candidates. Any command containing a pipe, `&&`, `||`, `;`, a redirection, `$(...)`, backticks, a newline or a leading `VAR=` is left alone.
- `rtk` is looked up with the Env-aware `LookPath`. Missing `rtk` means pass-through.
- `AllowedCommands` of `LocalShell` is checked on the **original** command, before any rewrite.
- `LocalShell.Rewrite` is applied after that check, to the command that is executed. Nil keeps the old behaviour.
- RTK keeps the exit code, and the error lines of `go vet` survive (tested).

## Compression middleware

`middleware.NewCompression(decider)` (`agents/middleware`) is a `WrapModelCall` transform:

- It builds the `Summary`, asks the guarded decider, and applies Caveman to old Human and AI turns, and the reply directive after the last Human message.
- `Keep` is the number of recent turns left alone (default **4**). The confidence floor (**0.7**) is set on `compress.Guarded`, not on `Compression`: `NewCompression` wraps the decider with `NewGuarded`. To change it, build a `compress.Guarded{...}` and pass it as `Compression{Decider: g}`.
- Tool calls and tool results are never touched, so each tool-call id and the order of pairs survive.
- Only the per-call copy changes. Stored state and checkpoints keep the original text.
- The reply directive is placed after the last user message, not in the system prompt, so byte-stable prefixes (`cag`) keep their cache.
- `Summarization` stays a separate `BeforeModel` middleware, not a step inside Compression (the hook order makes "only if still over after Caveman" impossible). Set its `MaxTokens` above the decider's threshold so it acts as a **backstop** for histories that Caveman cannot shrink enough.

## HTTPDecider: Laya server

`compress.HTTPDecider{URL, APIKey, Model, Client}` asks a Laya server over HTTP with `net/http`, no third-party code. Default URL: `http://localhost:8000/v1/systemone`.

The official server is `laya-serve`, a Python package:

```bash
pip install "laya[serve]"
LAYA_MODELS=... LAYA_DEVICE=cpu LAYA_HOST=127.0.0.1 LAYA_PORT=8000 LAYA_API_KEY=... laya-serve
```

Environment variables: `LAYA_MODELS` (models to load), `LAYA_DEVICE`, `LAYA_HOST`, `LAYA_PORT`, `LAYA_API_KEY` (sent as the bearer key). The `laya serve --model` form is not in the public docs and is not used here.

**Privacy:** the request carries the `Summary` state, including its tail: the last 3 messages, each cut to 160 bytes of JSON-encoded text, tool output included. The tail goes to the decider URL, so use a trusted endpoint. The middleware always uses the default `SummaryOptions`, and `TailMessages` 0 means the default 3, not none. To keep the tail on your machine, use a local decider (`RuleDecider`, or the native laya-go decider), not `HTTPDecider`.

The wire contract is `POST /v1/systemone`: a JSON body with the state and the typed questions, and a JSON reply with typed answers. The protocol is the same as TypeSafe JEV's, so a JEV endpoint should work by URL and key. **That is untested.**

**Open point:** the Noul response shape of `laya-serve` (boolean with or without a probability) is not documented here. A boolean with no confidence falls back to the rules through the guard. Check a real response before relying on Laya's probabilities.

The example `decider_laya_http` runs offline against an in-process fake server. With `LAYA_URL` (and `LAYA_API_KEY`, `LAYA_MODEL`) it uses a real server.

## Native Laya (laya-go)

`github.com/metalagman/laya-go` v0.2.6 runs the model in-process. It needs cgo and ONNX Runtime. The root `go.mod` requires it, but it is compiled only with the `laya_native` build tag, in `examples/langgraph/full_team/native`, a package of the root module. Its qualification: **linux/amd64, FP32 only**. Input longer than the model window returns `ErrInputTooLong` unless truncation is on.

Example: `examples/langgraph/decider_laya_native` is a separate Go module (own `go.mod`, `replace` to the repo root). Without the tag or with no `LAYA_BUNDLE_DIR`, it prints why and uses `RuleDecider`. Without the tag, `go build ./...` does not compile laya-go and does not need cgo.

## JEV (planned)

TypeSafe JEV is the second System 1 model. It is named here as planned and **not built**: there is no `decider_jev` folder and no code. JEV's public page states no API, Go support or window size. The HTTP client above is the path to try first.

## Examples

All run offline with a fake model unless a real service is configured. Each has `README.md`, `README.pt-BR.md` and `README.es-ES.md`.

| Example | Shows |
|---|---|
| `langgraph/decider_fallback` | the `Decider` interface, `RuleDecider` and `Guarded`, no model |
| `langgraph/compress_history_summary` | `Summary` of a history above 8k tokens, fits the 700-token budget |
| `langgraph/compress_shell_rtk` | shell output through RTK when the decider says so |
| `langgraph/compress_prompt_caveman` | prompt and reply compression with the middleware |
| `langgraph/decider_laya_http` | `HTTPDecider` against `laya-serve` (fake server offline) |
| `langgraph/decider_laya_native` | native laya-go, separate module, `laya_native` tag |
| `langgraph/full_team` | the team of `independent_engineering_team` with call compression; fake decider and model by default. With `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`, the shell tool offers only `ls`, `grep` and `echo` |

Test in production mode: set the real decider URL or the native bundle, and run `go test -race -count=1` in the example's module.
