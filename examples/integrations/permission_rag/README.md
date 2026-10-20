# permission_rag

A retriever that only returns documents the caller is allowed to read.

## What it does

- A stand-in policy server plays the role of Permit.io's PDP: `alice` may read the handbook and salaries, `bob` only the handbook.
- `permit.FilterRetriever` wraps the vector-store retriever and asks the policy engine about each hit, so Bob never sees the salary document. With SpiceDB the code is the same using `integrations/spicedb.FilterRetriever`.

## How to apply it

```go
inner := retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 5})
r := permit.FilterRetriever(inner, &permit.Client{PDPURL: url, APIKey: key}, user, "read", "doc", idOf)
```

Filter **before** the text reaches the model: once a restricted passage is in the prompt, the model can leak it. Put the document id in metadata at index time so `idOf` can find it.

## Run it

```bash
go run ./examples/integrations/permission_rag
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Point the client at your real Permit.io PDP (or SpiceDB) and model the same resources and roles your application uses.
2. Test with a matrix of users and documents, including a user with no permissions and a document with no policy (deny by default).
3. Over-fetch (`K` larger than needed) because filtering removes hits, then trim to the final count.
4. Log denied document ids per request for audit, never their content.
5. Treat the policy call as a dependency: set a timeout and fail closed.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 14: Production, security and troubleshooting](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
