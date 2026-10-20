# kag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Perguntas multi-hop respondidas percorrendo um grafo de conhecimento em vez de recuperar o melhor chunk isolado.

## O que faz

- Três chunks viram um grafo por `kag.Extractor` (entidades e relações tipadas; o fake devolve extrações estruturadas).
- A pergunta "quem prescreveu um remédio que interage com a aspirina prescrita a João?" precisa de três saltos entre chunks. A busca vetorial com `K=1` devolve um chunk e não consegue respondê-la.
- `kag.Engine{Verify: true}` planeja os passos, percorre as relações, verifica a resposta contra as triplas usadas e as devolve como fontes. Um último caso mostra um grafo cíclico terminando (arestas visitadas não se repetem).

## Como aplicar

```go
g := kag.NewMemory()                                   // or integrations/graphstore for Neo4j/Memgraph
(&kag.Extractor{Model: m, Embedder: emb, Graph: g}).Extract(ctx, chunks)
ans, _ := (&kag.Engine{Model: m, Graph: g, Verify: true, MaxHops: 4}).Answer(ctx, knowledge.Question{Text: q})
```

Use KAG para perguntas sobre relações ("quem ... está ligado a ...", cadeias de dependências). Tipos de relação e rótulos são validados (`^[A-Z][A-Z0-9_]*$`) antes de chegarem a um graph store; nunca concatene texto do modelo numa query.

## Como rodar

```bash
go run ./examples/knowledge/kag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo real com tool/function calling (extração, planejamento e verificação são chamadas estruturadas) e um embedder real.
2. Troque `kag.NewMemory()` por um graph store de `integrations/graphstore` (Neo4j ou Memgraph); a suíte de conformidade em `internal/conformance` mostra o que cada store precisa satisfazer.
3. Audite a qualidade da extração numa amostra (entidades unidas corretamente, tipos de relação sensatos) antes de confiar nas respostas; confira as contagens de `Dropped`.
4. Limite a travessia (`MaxHops`) e confira que perguntas sem resposta devolvem "não sei".
5. Mantenha `Verify` ligado para qualquer coisa voltada ao usuário.

## Veja também

- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
