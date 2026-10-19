# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Go port of the LangChain ecosystem (module `github.com/fsvxavier/golang-chain-eco`, Go 1.27). Docs and README are in pt-BR; keep that language when editing them. `_old/` and `.kilo/` are ignored reference material, not part of the build.

## Commands

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...                         # everything; real-service tests skip themselves
go test -race -run TestName ./path/to/pkg            # single test
go test ./internal/sqlitetest/...                    # sqlsaver against real SQLite
PG_DSN=... go test ./internal/pgtest/...             # PostgreSQL/MariaDB/CockroachDB/Oracle
go run ./examples/langgraph/<name>                   # every example is a main package; runs offline with a fake model
go run ./cmd/ciam <index|search|decision|mcp|...>    # ciam CLI
go run ./cmd/knowledge <index|ask|mcp|serve>         # RAG/CAG/KAG CLI
NEO4J_URL=... MEMGRAPH_ADDR=... go test ./integrations/graphstore   # graph stores against real Neo4j/Memgraph (Docker)
```

Integration tests for external services skip unless their env var is set (`REDIS_ADDR`, `MONGO_ADDR`, `K8S_SERVER`, ...); they share the suite in `internal/conformance`.

## Rules that are easy to break

- **Library code is stdlib-only.** No production package may import a third-party module. SQL drivers in `go.mod` are used only by tests under `internal/pgtest` and `internal/sqlitetest`.
- Examples must run offline (fake model); a real provider is used only when `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` is set.
- When adding a package or example, update the package map and counts in `README.md`, `docs/ARCHITECTURE.md`, `docs/manual/README.md` and `examples/README.md`.

## Architecture

Layers build on each other; higher layers reuse lower ones rather than reimplementing them.

- `core/`: Runnable[I, O] generics, messages, tools (`tools.New[Args]`), callbacks, embeddings, runnables config (`runnables.Config`, carried in `context`).
- `langgraph/`: `graph` (StateGraph, Pregel-style super-steps, `Compiled.Invoke/Stream/GetState/Saver`, `Command`, `interrupt`), `checkpoint` (`Saver` interface, in-memory impl; SQL and other backends live in `integrations/`). `graph.State` is an alias of `map[string]any`. Resume after interrupt = `graph.Command{Resume: ...}` on the same thread (`graph.Thread(id)`).
- `agents/`: `create_agent` equivalent with a middleware chain. `deepagents/`: planning, filesystem, subagents on top of it.
- `langgraph/store`: key-value store used by langmem and ciam. `mcp/` client, `mcp/server` generic MCP server (serves any `[]tools.Tool`). `remote/` RemoteGraph client plus `remote/server` (LangGraph Platform subset).
- `langmem/`: long-term memory (extractors, `IndexedStore`).
- `agentprotocol/`: full Agent Protocol v0.1.6 server and client. A per-thread hub (ring buffer, replay by `seq`) fed by `translator` (translate.go) which maps graph events and callbacks to streaming-protocol channels. `streamMemory` is shared per thread so resumed runs do not replay messages and tool-call ids stay correlated.
- `ciam/`: project-context manager (incremental hash index, hybrid cosine + BM25 search with RRF, ADR/PRD/Plan docs, memory via langmem). `ciam.Toolkit` exposes everything as `ciam_*` tools and as an MCP server; `cmd/ciam` is the CLI.
- `openswe/`: Open SWE port, composed from the pieces above. Subpackages: `github` (REST client, App JWT), `sandbox` (per-thread, never silently replaced), `guards` (middleware guard rails), `review`, `babysit` (CI watcher), `triggers`, `prompts` (embedded templates). `openswe.Config.Context` takes a `*ciam.Toolkit`; `agentprotocol` can serve the openswe graphs.
- `knowledge/`, `rag/`, `cag/`, `kag/`: RAG, CAG and KAG behind one `knowledge.Answerer` (question in, sourced answer + `Trace` out). `knowledge.Router` picks the technique and `knowledge.Toolkit` serves them as tools/MCP (`cmd/knowledge` is the CLI). `rag` composes `core/retrievers` + `integrations/retrieval`; `cag` keeps the corpus in a byte-stable system prefix (`cache_control` for Anthropic; `messages.Usage` has `CacheReadTokens`/`CacheCreationTokens`); `kag` keeps a graph behind `kag.GraphStore` (in-memory, plus `integrations/graphstore` for Neo4j/Memgraph; `internal/conformance.Graph` is the shared suite).
- `providers/`, `chatmodels/`, `integrations/`, `tracing/`: model providers, vector stores/savers/object stores, LangSmith.

## Gotchas

- Tool argument schemas validate lists as `[]any`, not `[]string`; tests must pass `[]any`.
- Tool-call ids must be unique within a conversation; a repeated id fails the run.
- Agents with middleware hooks hit the default recursion limit (25) fast; openswe runs use `RecursionLimit` 2000.
- GitHub branch names keep `/` unescaped in API paths (`escRef` in `openswe/github`); escaping to `%2F` 404s.
- Graph `updates` events carry `{node: update}`; use `unwrapUpdate` (agentprotocol) instead of reading the payload directly.
- Relation types and labels cannot be Cypher parameters: graph adapters validate them (`^[A-Z][A-Z0-9_]*$`) before interpolating, and `kag.Extractor` normalises LLM output to that form. Never concatenate model text into a query.
- Examples that need GitHub use the in-process fake at `examples/internal/fakegithub`.

<!-- rtk-instructions v2 -->
# RTK (Rust Token Killer) - Token-Optimized Commands

## Golden Rule

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is always safe to use.

**Important**: Even in command chains with `&&`, use `rtk`:
```bash
# ❌ Wrong
git add . && git commit -m "msg" && git push

# ✅ Correct
rtk git add . && rtk git commit -m "msg" && rtk git push
```

## RTK Commands by Workflow

### Build & Compile (80-90% savings)
```bash
rtk cargo build         # Cargo build output
rtk cargo check         # Cargo check output
rtk cargo clippy        # Clippy warnings grouped by file (80%)
rtk tsc                 # TypeScript errors grouped by file/code (83%)
rtk lint                # ESLint/Biome violations grouped (84%)
rtk prettier --check    # Files needing format only (70%)
rtk next build          # Next.js build with route metrics (87%)
```

### Test (60-99% savings)
```bash
rtk cargo test          # Cargo test failures only (90%)
rtk go test             # Go test failures only (90%)
rtk jest                # Jest failures only (99.5%)
rtk vitest              # Vitest failures only (99.5%)
rtk playwright test     # Playwright failures only (94%)
rtk pytest              # Python test failures only (90%)
rtk rake test           # Ruby test failures only (90%)
rtk rspec               # RSpec test failures only (60%)
rtk test <cmd>          # Generic test wrapper - failures only
```

### Git (59-80% savings)
```bash
rtk git status          # Compact status
rtk git log             # Compact log (works with all git flags)
rtk git diff            # Compact diff (80%)
rtk git show            # Compact show (80%)
rtk git add             # Ultra-compact confirmations (59%)
rtk git commit          # Ultra-compact confirmations (59%)
rtk git push            # Ultra-compact confirmations
rtk git pull            # Ultra-compact confirmations
rtk git branch          # Compact branch list
rtk git fetch           # Compact fetch
rtk git stash           # Compact stash
rtk git worktree        # Compact worktree
```

Note: Git passthrough works for ALL subcommands, even those not explicitly listed.

### GitHub (26-87% savings)
```bash
rtk gh pr view <num>    # Compact PR view (87%)
rtk gh pr checks        # Compact PR checks (79%)
rtk gh run list         # Compact workflow runs (82%)
rtk gh issue list       # Compact issue list (80%)
rtk gh api              # Compact API responses (26%)
```

### JavaScript/TypeScript Tooling (70-90% savings)
```bash
rtk pnpm list           # Compact dependency tree (70%)
rtk pnpm outdated       # Compact outdated packages (80%)
rtk pnpm install        # Compact install output (90%)
rtk npm run <script>    # Compact npm script output
rtk npx <cmd>           # Compact npx command output
rtk prisma              # Prisma without ASCII art (88%)
rtk uv run <cmd>        # Compact uv project command output
```

### Files & Search (60-75% savings)
```bash
rtk ls <path>           # Tree format, compact (65%)
rtk read <file>         # Code reading with filtering (60%)
rtk grep <pattern>      # Search grouped by file (75%). Format flags (-c, -l, -L, -o, -Z) run raw.
rtk find <pattern>      # Find grouped by directory (70%)
```

### Analysis & Debug (70-90% savings)
```bash
rtk err <cmd>           # Filter errors only from any command
rtk log <file>          # Deduplicated logs with counts
rtk json <file>         # JSON structure without values
rtk deps                # Dependency overview
rtk env                 # Environment variables compact
rtk summary <cmd>       # Smart summary of command output
rtk diff                # Ultra-compact diffs
```

### Infrastructure (85% savings)
```bash
rtk docker ps           # Compact container list
rtk docker images       # Compact image list
rtk docker logs <c>     # Deduplicated logs
rtk kubectl get         # Compact resource list
rtk kubectl logs        # Deduplicated pod logs
```

### Network (65-70% savings)
```bash
rtk curl <url>          # Compact HTTP responses (70%)
rtk wget <url>          # Compact download output (65%)
```

### Meta Commands
```bash
rtk gain                # View token savings statistics
rtk gain --history      # View command history with savings
rtk discover            # Analyze Claude Code sessions for missed RTK usage
rtk proxy <cmd>         # Run command without filtering (for debugging)
rtk init                # Add RTK instructions to CLAUDE.md
rtk init --global       # Add RTK to ~/.claude/CLAUDE.md
```

## Token Savings Overview

| Category | Commands | Typical Savings |
|----------|----------|-----------------|
| Tests | vitest, playwright, cargo test | 90-99% |
| Build | next, tsc, lint, prettier | 70-87% |
| Git | status, log, diff, add, commit | 59-80% |
| GitHub | gh pr, gh run, gh issue | 26-87% |
| Package Managers | pnpm, npm, npx | 70-90% |
| Files | ls, read, grep, find | 60-75% |
| Infrastructure | docker, kubectl | 85% |
| Network | curl, wget | 65-70% |

Overall average: **60-90% token reduction** on common development operations.
<!-- /rtk-instructions -->