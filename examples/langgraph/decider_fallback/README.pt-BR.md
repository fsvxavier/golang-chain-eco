# decider_fallback

[English](README.md) | **Português** | [Español](README.es-ES.md)

Responde as perguntas sobre o histórico com `compress.RuleDecider` e mostra `compress.Guarded` voltando para essas regras quando o decider do modelo falha.

## O que faz

- Monta um histórico curto (4 mensagens) e um longo (120 mensagens, cerca de 12 mil tokens estimados).
- `compress.Summarize(...).State()` gera a visão em objeto JSON que os deciders leem.
- `RuleDecider{}` responde `compress_history`, `level` e `compress_reply` com limiares fixos (comprimir acima de 4000 tokens estimados, nível `full`). Não precisa de modelo.
- `compress.Guarded` envolve um `FuncDecider` que sempre retorna erro. Todas as perguntas caem nas regras, então a execução não falha.

## Como aplicar

```go
st := compress.Summarize(msgs, compress.SummaryOptions{}).State()
g := compress.NewGuarded(modelDecider) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

Use `Guarded` sempre que um modelo decidir algo no caminho da requisição. Uma resposta lenta, errada ou ausente custa qualidade, nunca a execução. A resposta só é usada quando o id e o tipo batem e a probabilidade é pelo menos o limiar.

## Como executar

```bash
go run ./examples/langgraph/decider_fallback
```

A saída é a mesma em toda execução. O histórico curto mantém a transcrição completa, o longo é comprimido, e a execução protegida imprime as respostas das regras depois da falha do modelo.

## Testar em modo de produção

1. Troque `modelDecider` por um `compress.HTTPDecider` real (veja `compress/httpdecider.go`) e confira o limiar e o timeout com a latência do seu modelo.
2. Registre quais perguntas caíram no fallback. Uma taxa crescente indica decider quebrado ou lento.
3. Mantenha o `Guarded` em volta do decider. Não chame o decider do modelo diretamente.
4. Rode `go test -race -count=1 ./compress` depois de mudar os limiares.

## Veja também

- [Exemplo de resumo do histórico](../compress_history_summary/README.pt-BR.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
