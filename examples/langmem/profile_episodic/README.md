# profile_episodic

The three shapes of long-term memory: a collection of facts, a single strict profile, and episodes of how a situation was handled.

## What it does

- **Collection**: facts accumulate, change and disappear. The second pass updates "Likes dark mode" to "Dislikes dark mode" and deletes "Lives in Rome" (`RemoveDoc`).
- **Profile**: one document with a Go struct schema (`UserProfile`), refined in place; inserts and deletes are disabled and the example decodes it with `langmem.Decode[UserProfile]`.
- **Episodic**: `EpisodeSchema` stores observation, thoughts, action and result, so the agent can reuse an approach that worked.

## How to apply it

```go
sm := langmem.NewStoreManager(model, nil, langmem.WithStore(st))                       // collection
pm := langmem.NewStoreManager(model, nil, langmem.WithStore(st),
    langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save preferences.")),
    langmem.WithInserts(false), langmem.WithDeletes(false),
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))                        // profile
em := langmem.NewStoreManager(model, nil, langmem.WithStore(st), langmem.WithSchemas(langmem.EpisodeSchema)) // episodes
```

Choose by the question you ask later: "what do I know about this user?" (collection), "what are this user's preferences?" (profile), "how did we solve something like this?" (episodes).

## Run it

```bash
go run ./examples/langmem/profile_episodic
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model: the extractor must follow the tool schemas (`Memory`, `RemoveDoc`, your profile struct); check on real conversations that it updates instead of duplicating.
2. Use a durable store and a real embedder; give the manager a `user_id` namespace from the authenticated user.
3. Validate profile fields after extraction (length, allowed values) and keep PII policy in mind: provide read, correct and delete for users.
4. Review a sample of memories weekly; bad extractions compound.

## See also

- [Manual, chapter 16: Long-term memory](../../../docs/en-US/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/en-US/LANGMEM.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
