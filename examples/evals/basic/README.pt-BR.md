# basic

[English](README.md) | **Português** | [Español](README.es-ES.md)

Pontue um sistema sobre um dataset JSONL com avaliadores de igualdade exata, contains, embedding e LLM-as-judge e imprima um relatório em texto e JSON.

## O que faz

- Grava e recarrega um dataset JSONL de três perguntas (`evals.SaveJSONL`/`LoadJSONL`).
- Roda o alvo (um modelo aqui) em cada exemplo com concorrência 3 e o pontua com `ExactMatch`, `Contains`, `EmbeddingDistance` e `LLMJudge`.
- Imprime `evals.WriteText` e `evals.WriteJSON`: a igualdade exata falha em "It is 4." enquanto contains e o juiz passam, o que mostra por que se combinam avaliadores.

## Como aplicar

```go
report, _ := evals.Evaluate(ctx, run, dataset, []evals.Evaluator{
    evals.ExactMatch("answer"), evals.Contains("answer"),
    evals.EmbeddingDistance(embedder, "answer", 0.8),
    evals.LLMJudge(judge, "The answer must be a correct, direct reply."),
}, evals.WithConcurrency(8))
```

`run` pode ser qualquer função de entradas para saídas: uma chain, um agent, uma chamada HTTP. Mantenha o dataset em controle de versão ao lado dos prompts.

## Como rodar

```bash
go run ./examples/evals/basic
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Monte um dataset com perguntas reais (algumas dezenas para começar) e saídas conferidas por uma pessoa.
2. Aponte `run` para o sistema que você implanta (o mesmo agent, prompt e modelo) e use um modelo diferente como juiz.
3. Use um embedder real em `EmbeddingDistance`.
4. Rode no CI a cada mudança de prompt ou modelo; quebre o build quando uma métrica cair abaixo de um limite e salve o relatório JSON como artefato.
5. Rode de novo os casos instáveis várias vezes e olhe a variância antes de confiar num delta.

## Veja também

- [EVALS.md](../../../docs/pt-BR/EVALS.md)
- [Manual, capítulo 12: Observabilidade](../../../docs/pt-BR/manual/12-observabilidade.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
