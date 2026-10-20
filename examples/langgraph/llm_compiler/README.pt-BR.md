# llm_compiler

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um planner emite um DAG de chamadas de tool; cada chamada cujas dependências terminaram roda em paralelo com `Send`, e então um joiner monta a resposta.

## O que faz

- O plano tem três tarefas: população da França, população da Alemanha (independentes) e a soma delas (depende das duas).
- `schedule` encontra as tarefas com dependências completas e as distribui com `graph.Send`; os resultados se juntam em `done` por um reducer `BinOp` próprio.
- As duas chamadas independentes rodam em paralelo (o exemplo imprime o tempo decorrido), a soma roda depois e `join` formata a resposta.

## Como aplicar

Use quando um modelo consegue planejar várias chamadas de tool independentes de antemão: a latência passa a ser a profundidade do DAG, não o número de chamadas.

```go
sends = append(sends, graph.Send{Node: "exec", Arg: graph.State{"task": t, "deps": done}})
return sends, nil // a conditional edge returning []Send fans out
```

Aqui o planner é fixo; num sistema real um modelo devolve o plano em JSON, que você valida (ids únicos, dependências existentes, sem ciclos) antes de agendar.

## Como rodar

```bash
go run ./examples/langgraph/llm_compiler
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Deixe um modelo real produzir o plano e valide-o com rigor; rejeite ciclos e tools desconhecidas.
2. Troque o sleep por chamadas de tool reais, cada uma com timeout, retry e limite de tamanho do resultado.
3. Limite o paralelismo (um semáforo) para que um plano grande não inunde uma API a jusante.
4. Decida o que uma tarefa que falhou significa para as dependentes (pular, replanejar ou abortar) e teste.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
