# middleware_stack

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent envolto nos middlewares que você quer em produção: redação de PII, retry, fallback de modelo e limites de chamadas.

## O que faz

- O modelo primário sempre falha com 503; o modelo de backup funciona.
- `PII` oculta o e-mail antes que qualquer modelo o veja. `ModelRetry` tenta o primário duas vezes e `ModelFallback` troca para o backup.
- `ModelCallLimit` e `ToolCallLimit` limitam quantas chamadas uma execução pode fazer.
- Imprime a mensagem armazenada do usuário, o número de tentativas no primário e a resposta do backup, o que confirma que o e-mail chegou redigido.

## Como aplicar

Componha `agents.Middleware` em ordem; o primeiro é o mais externo.

```go
agents.Create(agents.Options{
    Model: primary, Tools: tools,
    Middleware: []agents.Middleware{
        middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail}},
        middleware.ModelRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
        middleware.ModelFallback{Fallbacks: []models.ChatModel{backup}},
        middleware.ModelCallLimit{RunLimit: 25},
        middleware.ToolCallLimit{RunLimit: 10},
    },
})
```

A ordem importa: oculte primeiro para que retries e fallbacks nunca vejam PII bruta, tente de novo antes de cair no fallback e deixe os limites por último para que contem chamadas reais.

## Como rodar

```bash
go run ./examples/langchain/middleware_stack
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Troque os dois fakes por modelos reais de dois provedores (por exemplo Anthropic como primário e OpenAI como fallback).
2. Force uma falha (chave inválida ou `httptest` devolvendo 503 no primário) e confirme que o fallback responde e que o log mostra os retries.
3. Envie PII realista (e-mails, números de cartão) e inspecione a requisição que chega a cada provedor para confirmar que está redigida.
4. Defina `RunLimit` a partir de um p99 medido de chamadas por tarefa, não de um palpite, e alerte quando uma execução o atingir.
5. Adicione `Summarization` para threads longas e um `RecursionLimit` explícito em `runnables.Config` (veja o checklist).

## Veja também

- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Manual, capítulo 14: Produção, segurança e troubleshooting](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
