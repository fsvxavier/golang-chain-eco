# compress_history_summary

[English](README.md) | **Português** | [Español](README.es-ES.md)

Monta um histórico longo (40 turnos com resultados de tools, cerca de 13 mil tokens estimados) e mostra o `compress.Summary` limitado que os deciders leem no lugar da transcrição.

## O que faz

- `compress.Summarize(msgs, compress.SummaryOptions{})` conta mensagens, turnos, tokens, o maior resultado de tool e a fração de bytes de tools, detecta o idioma e mantém as últimas 3 mensagens cortadas em 160 bytes cada.
- `Summary.EncodedTokens()` mede o JSON de `Summary.State()` à razão de 4 bytes por token. Precisa ficar em até 700 tokens (o orçamento). O exemplo termina com erro se não ficar.
- O exemplo imprime a contagem de mensagens, os tokens do histórico, os tokens do resumo e o JSON do resumo.

## Como aplicar

```go
s := compress.Summarize(msgs, compress.SummaryOptions{})
answers, err := compress.NewGuarded(modelDecider).Decide(ctx, s.State(), compress.HistoryQuestions())
```

Envie o Summary ao decider, não o histórico. O tamanho dele não cresce com o histórico: uma mensagem de 5 MB custa o mesmo que uma curta, porque só um prefixo limitado de cada mensagem é lido.

## Como executar

```bash
go run ./examples/langgraph/compress_history_summary
```

Linhas de saída esperadas: `messages: 120`, `history tokens: 13160` (ou próximo), `summary tokens: 130 (budget 700)`, e depois o JSON `summary:` com `messages`, `turns`, `tokens`, `tool_share`, `largest_tool_bytes`, `code_share`, `json_tool_bytes`, `lang` e `tail`.

## Testar em modo de produção

1. Use uma transcrição real e confira se `summary tokens` fica abaixo do orçamento nas sessões maiores.
2. Confira `lang` em históricos com mais de um idioma. O detector lê só 4 KiB por mensagem.
3. Mantenha o `tail` do resumo curto. Ele leva os turnos mais recentes, então um tail errado é uma decisão errada.
4. Rode `go test -race -count=1 ./compress` depois de mudar o orçamento ou as opções.

## Veja também

- [Exemplo de fallback de decider](../decider_fallback/README.pt-BR.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
