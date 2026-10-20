# cag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Geração aumentada por cache: o corpus inteiro vai num prefixo de system prompt estável byte a byte, que o provedor guarda em cache entre chamadas.

## O que faz

- `cag.NewCorpus` calcula o hash dos documentos e estima os tokens; `cag.Engine` os coloca no system prompt.
- A pergunta 1 reporta `CacheCreationTokens`, a pergunta 2 reporta `CacheReadTokens` (o passo de trace `cache_hit`), como um provedor com cache de prefixo faz.
- Com uma `Window` deliberadamente pequena, o engine não consegue encaixar o corpus e recorre a um respondedor por palavras-chave (o passo de trace `fallback`).

## Como aplicar

```go
corp := cag.NewCorpus(docs)
engine := &cag.Engine{Model: model, Corpus: corp, Fallback: ragOrKeywordAnswerer}
ans, _ := engine.Answer(ctx, knowledge.Question{Text: q})
```

Use CAG quando o corpus é pequeno (precisa caber na janela de contexto), estável e consultado com frequência: não há etapa de recuperação e os tokens de entrada em cache custam muito menos. Mantenha o prefixo idêntico byte a byte entre as chamadas (mesma ordem, mesmo texto); o engine o marca com `cache_control` para a Anthropic.

## Como rodar

```bash
go run ./examples/knowledge/cag
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use a Anthropic (`ANTHROPIC_API_KEY`) com um corpus real; faça as mesmas duas perguntas duas vezes e confirme que `cache_read` é diferente de zero na segunda chamada (essa é a economia).
2. Atenção à vida útil do cache: um prefixo frio após o TTL paga o preço cheio. Meça o custo por pergunta num padrão de tráfego realista.
3. Reconstrua o corpus (novo hash) só quando os documentos mudarem; um byte alterado invalida o cache.
4. Defina `Window`/`Reserve` para o modelo que você usa e forneça um `Fallback` real (em geral o pipeline RAG) para quando o corpus ultrapassar a janela.

## Veja também

- [Manual, capítulo 20: RAG, CAG e KAG](../../../docs/pt-BR/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
