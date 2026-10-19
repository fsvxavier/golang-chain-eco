# Knowledge: RAG, CAG e KAG

Referência dos pacotes `knowledge`, `rag`, `cag` e `kag`. Desenho completo em [DESIGN-RAG-CAG-KAG.md](../DESIGN-RAG-CAG-KAG.md).
O código de biblioteca usa só a stdlib; modelo, embedder, vector store e grafo entram por interface.

## `knowledge`: contrato comum

Toda técnica implementa `knowledge.Answerer`:

```go
type Answerer interface {
    Answer(ctx context.Context, q Question) (Answer, error)
}
```

| Tipo | Campos | Uso |
|---|---|---|
| `Question` | `Text`, `History []messages.Message`, `Filter vectorstores.Filter`, `Force string` | `Force` vale `""` (automático), `"rag"`, `"cag"` ou `"kag"` |
| `Answer` | `Text`, `Sources []Source`, `Strategy`, `Usage *messages.Usage`, `Trace []Step` | `Strategy` diz quem respondeu; `Trace` registra as decisões |
| `Source` | `ID`, `Content`, `Metadata`, `Score` | evidência usada na resposta |

Funções auxiliares:

- `knowledge.AsRunnable(a)` expõe um `Answerer` como `runnables.Runnable[Question, Answer]`.
- `knowledge.SourcesFromDocs(docs)` converte documentos recuperados em `Source` (o `Score` vem de `metadata["score"]` quando é `float64`).
- `knowledge.Prompt(system, fontes, q)` monta as mensagens: system com o contexto numerado (`[1]`, `[2]`...), histórico e a pergunta.
  `knowledge.DefaultSystem` pede resposta só com base no contexto e citação `[n]`. `Prompt` não troca `system` vazio por `DefaultSystem`.
- `knowledge.ErrEmptyQuestion` é devolvido para pergunta sem texto.

### Contagem de tokens

`knowledge.TokenCounter` (`Count(text string) int`) é substituível. O padrão `knowledge.Heuristic{CharsPerToken: 4}` conta runas
e arredonda para cima (`""` dá 0). É uma estimativa, não um tokenizer: use um contador próprio quando a margem importar.

## Métricas de cache em `messages.Usage`

(Preenchido na Task 1: `CacheReadTokens` e `CacheCreationTokens`.)
