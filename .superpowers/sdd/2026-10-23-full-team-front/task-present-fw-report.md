# /present roll call and serve mode for examples/langgraph/full_team

Commits: 476089f (serve), 237dd2b (roll call), 2471a24 (docs).

## Design
- Model per worker: RunWorker already has `label` from NewModel. It is stored in `worker.model` and sent in the existing registration JSON as `WorkerInfo.Model` (`json:"model,omitempty"`, backward compatible). `HubClient.Register(ctx,id,role,url,model)` gained a parameter; `Hub.Register(id,role,url)` kept, new `Hub.RegisterInfo(WorkerInfo)`; the HTTP handler calls RegisterInfo. The roll takes the model from `Hub.State`, never from the LLM answer. Empty model shows as `unknown`.
- Prompt sent to each worker (Agent Protocol, same path as team_ask_worker, new thread each):
  `ROLL CALL: the teacher is checking attendance.` / `WORKER: <id> ROLE: <role>` / `Answer with ONE short line that starts with "Presente!" and names you (the worker id), your role and your model.`
- Brain: first check in `Brain()`: if prompt contains `ROLL CALL:` -> `Presente! <id> (<role>), <flavour>` (Portuguese flavour per role in `rollFlavour`). It passes through the compression + TOON + OfflineWithTool path (verified by TestServePresentOverStdio, which runs with the demo's middleware).
- `team_present` (bridge.go, `rollCall`/`askOne`): all hub workers in parallel, per-worker timeout `Bridge.PresentTimeout` (default 30 s), overall ctx = per + 5 s. Present = non-empty answer without error; absent = error, timeout, non-success run or empty. Row: `- <id> (<role>) [model <m>] present: <answer<=160>` or `absent: <reason>`; then `N of M present` and, if any, `Absent: id, id`.
- MCP prompt `present` (`presentPrompt()`, registered in `MCPServer`): no arguments, description "Roll call: every persona answers Presente"; text tells the chat to call `team_present`, print a school roll call, end with `N of M present` plus absent list, and say the integration is not working (showing the error) if the tool is missing or errors.
- Serve (serve.go, main.go `serve` arg): modelled on full_team_front: ctxReader copy, signal.NotifyContext + stop after first signal, log writer and `team.Console` (new var in services.go, nil = stdout, used by RunManager) routed to stderr and restored in a defer. `serveOptions(key)` = demo options without shell (compression meter middleware, open_tasks tool, OfflineWithTool). Random 32-byte hex key (crypto/rand) passed via new `LocalOptions.ManagerKey` -> `RunManager(ctx,hubURL,ln,apiKey)` (explicit key wins over MANAGER_API_KEY) and set on the bridge's `remote.Client.APIKey`. `StartLocalWith` readiness loop returns `ctx.Err()` when cancelled; serve treats `context.Canceled` there as a clean stop. Demo path unchanged (RunManager receives "" and reads the env as before).

## Sample roll (real, offline, captured by TestServePresentOverStdio over stdio pipes)
```
ROLL CALL
- architect (architect) [model offline] present: Presente! architect (architect), com o diagrama na mão
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- po (po) [model offline] present: Presente! po (po), com o backlog debaixo do braço
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
11 of 11 present
```

## Tests
RED: `go vet .` failed with `undefined: serve` (serve tests written first) and `unknown field PresentTimeout in struct literal of type Bridge` (present tests written first). GREEN: `go test -race -count=1 ./examples/langgraph/full_team/...` all ok; `go build ./...` and `go vet ./examples/langgraph/full_team/...` clean.
- serve_test.go: TestServeListsTeamTools, TestServeOptionsHaveNoShell, TestServeSubmitUsesTheGeneratedKey (operator key set to a bogus value; team_submit only works with the generated one), TestServePresentOverStdio (initialize, prompts/list, prompts/get, tools/call; every stdout line JSON-RPC; os.Stdout empty), TestServeStopsOnEOF, TestServeStopsOnContextCancel, TestServeRestoresGlobals.
- internal/team/present_test.go: TestPresentToolRollCall (11 of 11, rows with id/role/model/Presente!), TestPresentAbsentWhenWorkerDown (qa-2 cancelled, polls until 10 of 11, `Absent: qa-2`, PresentTimeout 2 s), TestPresentPromptListed, TestBrainRollCall, TestRegisterCarriesModel.

## Files
examples/langgraph/full_team: main.go, serve.go, serve_test.go, internal/team/{bridge,brain,hub,hubhttp,worker,services,local}.go, internal/team/present_test.go, README.md, README.pt-BR.md, README.es-ES.md. Nothing touched in full_team_front, independent_engineering_team or native (native still builds: vet ok).

## Unverified
- Slash spellings `/mcp__team__present` (Claude Code) and `/mcp.team.present` (Copilot Chat) are flagged UNVERIFIED in all three READMEs.
- `.mcp.json`, `claude mcp add` and `.vscode/mcp.json` shapes copied from full_team_front's README; not tested against real clients.
- Not run with a real model key; the per-role model list in the README comes from `anthropicDefaults` in models.go.

## Concerns
- `RunManager` and `HubClient.Register` signatures changed (internal package, only callers updated).
- A cancelled worker stays in the hub until its lease TTL (15 s), so it shows as `absent` (connection refused), which is the intended outcome.
- Present = non-empty answer; a real model that ignores the "Presente!" instruction still counts as present.
- Concurrent edits: during my work another commit (cdb8f44 "full_team_front: no shell in serve, ... manager key, cancel during startup") landed; it did not conflict with my files.

## What full_team_front must receive (port list)
1. hub.go: `WorkerInfo.Model` field; `Hub.RegisterInfo(WorkerInfo)` with `Register` delegating to it; keep Model on re-registration.
2. hubhttp.go: handler calls `RegisterInfo(wi)`; `HubClient.Register(..., model string)` sends Model.
3. worker.go: `worker.model = label`; pass `w.model` in `hub.Register` call.
4. brain.go: `ROLL CALL:` check at the top of `Brain` + `rollFlavour` + `rollCall` (pick flavours for front's roles: no architect; mid-2, senior-2 exist; consider front-end flavours).
5. bridge.go: `PresentTimeout` field, `team_present` tool, `rollCall`, `askOne`, `rollCallPrompt`, `presentPrompt()` and `.AddPrompt(presentPrompt())` in `MCPServer`; imports errors, sync, time.
6. Tests: present_test.go (adapt the Cast-derived counts; offline `model offline`), plus TestServePresentOverStdio in serve_test.go (its startServe helper already exists there). Docs: a /present section in front's README trio (slash spellings UNVERIFIED).

## Follow-up: roster, auth test, key scoping
- Roll call now merges the expected roster (`Bridge.Expected`, default `Cast`) with Hub.State: unregistered rostered worker = `absent: not registered (lease expired or never started)`; unlisted-in-roster registered worker = `extra (not in the roster, not counted)`; summary `N of M present`, M = roster size. Rows sorted by id, extras last. Existing present tests unchanged and passing. Lease expiry is simulated by a never-registered roster entry (TTL is fixed at 15 s), same code path as an expired lease.
- serve_test: raw unauthenticated POST to the manager (URL read from the `manager: up on` stderr line) gets 401/403 while team_submit works.
- `client(url)` sends the manager key only for an empty url or the manager's own base URL; other URLs get no key. Test checks the x-api-key header at an httptest server.
- Docs: /present section updated in README trio; healthy sample output unchanged and still real.
