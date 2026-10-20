# chain

[English](README.md) | **Português** | [Español](README.es-ES.md)

A menor chain possível: um prompt de chat, um modelo e um parser de saída encadeados (o equivalente do LCEL).

## O que faz

- Monta um prompt de chat de duas mensagens (`system` + `human`) com a variável `{topic}`.
- Encadeia `prompt | modelo | parser` com `runnables.Pipe3`, tipado de ponta a ponta: `prompts.Values` na entrada, `string` na saída.
- Invoca uma vez com `topic = "goroutines"` e imprime a resposta de uma frase.

## Como aplicar

Use este formato para qualquer chamada de LLM de um passo que precise ser testável e tipada.

```go
prompt := prompts.Chat([2]string{"system", "..."}, [2]string{"human", "Explain {topic}."})
chain  := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
    prompt, models.AsRunnable(model), outputparsers.Str{})
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

- Troque `outputparsers.Str{}` por um parser JSON ou estruturado quando quem chama precisar de dados, não de texto.
- Como todo estágio é um `Runnable`, dá para adicionar `WithRetry`, callbacks ou um tracer à chain inteira sem mexer nos estágios.

## Como rodar

```bash
go run ./examples/langchain/chain
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Exporte uma chave de provedor e rode o exemplo; a resposta deve ser uma frase sobre goroutines.
2. Envolva a chamada em `context.WithTimeout` e adicione `runnables.RetryOptions` para que um provedor lento ou com rate limit não trave quem chama.
3. Anexe um tracer (LangSmith, Langfuse ou OTLP; veja o capítulo 12) e confirme um trace por `Invoke` com o uso de tokens.
4. Coloque `models.NewRateLimiter` na frente do provedor se várias requisições compartilham a mesma chave.
5. Trate o texto do prompt em code review como qualquer outro código: ele é comportamento.

## Veja também

- [Manual, capítulo 4: Prompts, chains e parsers](../../../docs/pt-BR/manual/04-chains-prompts-parsers.md)
- [Manual, capítulo 3: Modelos e provedores](../../../docs/pt-BR/manual/03-modelos-e-provedores.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
