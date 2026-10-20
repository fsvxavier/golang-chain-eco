# content_builder

A deep agent that follows a brand-voice memory file and a skill to write a blog post into a real directory.

## What it does

- Creates a temp project with `AGENTS.md` (brand voice) and `skills/blog-post/SKILL.md`.
- `deepagents.Create` mounts the directory with `backends.Filesystem{Root: dir}`, loads `Memory: ["/AGENTS.md"]` and `Skills: ["/skills/"]`.
- The agent reads the skill, writes `/blog/go-concurrency.md`, and the example prints the file from disk and checks the system prompt carried the memory and the skill index.

## How to apply it

Use memory files for rules that always apply and skills for procedures loaded on demand.

```go
deepagents.Create(deepagents.Options{
    Model: model, Backend: backends.Filesystem{Root: projectDir},
    Memory: []string{"/AGENTS.md"}, Skills: []string{"/skills/"},
})
```

A skill is a folder with a `SKILL.md` (frontmatter `name` and `description`); the agent sees only the index until it reads one.

## Run it

```bash
go run ./examples/deepagents/content_builder
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use a real model and point `Root` at a real, version-controlled project directory (not a temp dir) so every change is a reviewable diff.
2. Run it on a branch, not on `main`: the agent writes real files.
3. Keep `Root` as narrow as possible; `Filesystem` confines paths to it, but anything inside is writable.
4. Review the generated post against the skill checklist (hook, three points, call to action) and tighten the skill when the model skips a step.
5. Keep secrets out of the memory and skill files; they are placed in the system prompt.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
