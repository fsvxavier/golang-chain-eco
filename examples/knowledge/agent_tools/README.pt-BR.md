# agent_tools

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent que responde a partir da base de conhecimento chamando tools de `knowledge.Toolkit` (`rag_answer`, ...). Os agents rodam com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail é adicionada quando a conversa tem uma parcela real de código, resultados JSON de tools viram TOON, tudo decidido por regras, sem precisar de modelo.

## O que faz

- Dois chunks (uma receita e uma nota de medicamento) são indexados num vector store em memória atrás de um `rag.Pipeline`.
- `knowledge.Toolkit` expõe o pipeline e um roteador como tools (`kb.All()`); o agent roteirizado chama `rag_answer`, recebe a resposta com citações e responde com `[1]`.

## Como aplicar

```go
kb := knowledge.Toolkit{RAG: rg, Router: &knowledge.Router{RAG: rg}}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kb.All(), SystemPrompt: "Answer from the knowledge base and cite sources."})
```

Dê conhecimento a um agent como tools quando ele deve decidir **quando** consultar. Adicione `CAG` e `KAG` ao toolkit e o roteador escolhe a técnica por pergunta.

## Como rodar

```bash
go run ./examples/knowledge/agent_tools
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use um embedder real e um store durável, e um modelo real para o pipeline e para o agent.
2. Indexe o seu corpus real com `cmd/knowledge index` e confira se as respostas citam as fontes certas.
3. Teste perguntas que o corpus não responde: o agent deve dizer isso em vez de chutar.
4. Mantenha os metadados de fonte (arquivo, seção) em todo chunk; as citações só são tão boas quanto os metadados.

## Veja também

- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
