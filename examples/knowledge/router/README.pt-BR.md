# router

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um roteador na frente de RAG, CAG e KAG que escolhe a técnica certa por pergunta, com rotas forçadas e fallback.

## O que faz

- Os mesmos três chunks alimentam um vector store (RAG), um corpus em cache (CAG) e um grafo extraído (KAG).
- Cinco cenários: uma pergunta multi-hop vai para o grafo; uma pergunta simples sobre um corpus pequeno e estável vai para o CAG; a mesma pergunta com um corpus volátil vai para o RAG; `Force: "kag"` sobrescreve a escolha; e quando o KAG falha, `Fallback: true` faz o RAG responder. Os passos de trace `route` e `fallback` mostram o porquê.

## Como aplicar

```go
r := &knowledge.Router{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine,
    Profile:  knowledge.Profile{HasGraph: true, CorpusTokens: n, ContextWindow: 200000, Volatile: false},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: q})
```

Descreva os seus dados com honestidade em `Profile` (tamanho, janela, volatilidade, existência de grafo): é isso que as regras de roteamento leem. Exponha o roteador a agents como tools ou MCP com `knowledge.Toolkit`.

## Como rodar

```bash
go run ./examples/knowledge/router
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos reais, um vector store e um graph store duráveis e defina `Profile` a partir de medições (tokens do corpus, com que frequência os documentos mudam).
2. Monte um conjunto rotulado de perguntas por técnica e confira se a rota escolhida bate; revise os roteamentos errados.
3. Mantenha `Fallback` ligado e teste-o quebrando o graph store (a resposta ainda deve chegar via RAG).
4. Registre o trace `route` por requisição e acompanhe a distribuição; uma mudança surpreendente indica que o perfil dos seus dados mudou.

## Veja também

- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
