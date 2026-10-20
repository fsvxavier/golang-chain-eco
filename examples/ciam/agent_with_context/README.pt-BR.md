# agent_with_context

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent que consulta o gerenciador de contexto do projeto antes de alterar código e, assim, respeita uma decisão que ninguém mencionou.

## O que faz

- Cria um projeto com um tipo de fatura e um ADR que diz "nunca use float para dinheiro".
- O agent tem as tools do `ciam` (`kit.All()`) e as instruções do ciam como system prompt. Antes de implementar, chama `ciam_decision_context` e recebe o código **e** o ADR, então propõe `DiscountCents int64` em vez de um percentual float.

## Como aplicar

```go
kit := ciam.Toolkit{M: mgr, Project: root}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

O system prompt (`ciam.Instructions`) diz ao agent quando consultar o contexto. Adicione as tools a qualquer agent, inclusive deep agents e o engenheiro do Open SWE.

## Como rodar

```bash
go run ./examples/ciam/agent_with_context
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use um modelo real e um projeto real indexado com um embedder real; confira se o modelo realmente chama `ciam_decision_context` primeiro (olhe o trace de chamadas de tool).
2. Semeie os ADRs que importam (dinheiro, autenticação, retenção de dados) e teste prompts que os violariam.
3. Meça os tokens por tarefa com e sem as tools.
4. Mantenha o índice atualizado (rode `Index` de novo após os merges).

## Veja também

- [Manual, capítulo 18: ciam: contexto de projeto barato e preciso para assistentes](../../../docs/pt-BR/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/pt-BR/CIAM.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
