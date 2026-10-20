# rubric

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um avaliador pontua a resposta final contra critérios e manda o agent de volta até passar (ou até o limite).

## O que faz

- O redator responde sem citação; o avaliador do middleware `Rubric` diz `needs_revision` com a lacuna "add a [source: url] citation".
- O agent revisa e o avaliador diz `satisfied`. O exemplo imprime cada avaliação e a resposta final.

## Como aplicar

```go
dmw.Rubric{Grader: graderModel, Criteria: []string{"cites a source"}, MaxIterations: 3}
```

Escreva critérios que um avaliador consiga responder com passou/não passou, um por linha. Use um modelo diferente (muitas vezes mais forte) como avaliador do que como redator e mantenha `MaxIterations` pequeno: cada volta custa uma chamada completa do modelo mais uma de avaliação.

## Como rodar

```bash
go run ./examples/deepagents/rubric
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos reais para redator e avaliador; o avaliador precisa devolver o formato JSON `{"result": ..., "criteria": [...]}`, então teste se um modelo real o segue.
2. Calibre a rubrica com 10-20 respostas boas e ruins conhecidas antes de confiar nela; registre as discordâncias.
3. Decida o que acontece quando `MaxIterations` é atingido sem `satisfied` (entregar com aviso, escalar para uma pessoa) e teste esse caminho.
4. Armazene `rubric_evaluations` com cada resposta para análise posterior.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
