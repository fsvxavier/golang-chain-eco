# compress_prompt_caveman

[English](README.md) | **Português** | [Español](README.es-ES.md)

Comprime com caveman os turnos antigos de um histórico longo, na cópia usada em cada chamada ao modelo, antes de cada chamada, usando `middleware.NewCompression`.

## O que faz

- Um histórico de 12 turnos (Human e AI, com um bloco de código Go em cada turno da AI) entra em `agents.Create`.
- `middleware.NewCompression(decider)` envolve o decider em `compress.NewGuarded`. O decider roteirizado responde sim: comprimir os turnos antigos em `full` e pedir resposta objetiva.
- Os últimos 4 turnos são mantidos como estão. Os turnos antigos perdem enchimento e artigos; cercas de código, código inline, caminhos, URLs e números continuam idênticos byte a byte.
- Um pequeno middleware `sniff`, posto depois da compressão, imprime o que o modelo recebeu de fato.
- As estimativas de tokens (`messages.ApproxTokens`) são impressas antes e depois. O histórico armazenado nunca é reescrito.

## Como aplicar

```go
agent, _ := agents.Create(agents.Options{
    Model:      model,
    Middleware: []agents.Middleware{middleware.NewCompression(decider)},
})
```

Coloque antes do middleware de sumarização; defina o `MaxTokens` da sumarização acima do limite do decider, para que ela atue só como reserva. Use `compress.RuleDecider{}` quando não houver modelo para decidir.

## Como rodar

```bash
go run ./examples/langgraph/compress_prompt_caveman
```

Sem chave de API o exemplo usa um modelo fake roteirizado, então roda offline. Com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` definida, o `examples/internal/demo` usa esse provedor para a resposta, e a compressão impressa é a mesma.

## Como testar em modo de produção

1. Troque o decider roteirizado por `compress.HTTPDecider` ou um modelo pequeno; mantenha `Floor` em `0.7`, para que uma resposta fraca volte para as regras.
2. Confira que chamadas de tool e resultados de tool não mudam (não mudam, mas confirme com suas próprias mensagens de tool).
3. Compare a qualidade das respostas com e sem compressão nas suas conversas reais antes de usar `ultra`.
4. Acompanhe a contagem de tokens nos seus traces; a economia depende de quanto do histórico é prosa.

## Veja também

- [Pacote compress](../../../compress/)
- [Exemplo: compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
