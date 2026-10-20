# self_discover

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um pipeline de quatro prompts: selecionar módulos de raciocínio, adaptá-los à tarefa, montar uma estrutura de raciocínio e resolver com ela.

## O que faz

- Quatro nós encadeados (`select`, `adapt`, `structure`, `reason`), cada um um prompt montado com a saída do anterior guardada no estado.
- Resolve o quebra-cabeça das duas cordas (medir 45 minutos) escolhendo primeiro uma abordagem de raciocínio e depois seguindo-a.

## Como aplicar

```go
step := func(key, tmpl string, from ...string) graph.NodeFunc { /* prompt = tmpl + values of the `from` keys */ }
AddNode("adapt", step("adapted", "ADAPT the selected modules to the task.", "selected", "task"))
```

Use em tarefas difíceis de raciocínio em que um pipeline fixo vence um prompt gigante. Cada saída intermediária é inspecionável, então dá para ver de onde veio uma resposta ruim.

## Como rodar

```bash
go run ./examples/langgraph/self_discover
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo forte e o seu próprio catálogo de módulos de raciocínio.
2. Valide a estrutura JSON do terceiro passo antes de resolver; tente de novo ou falhe se estiver malformada.
3. Compare acurácia e custo com um único prompt "pense passo a passo" em 20-30 tarefas; mantenha o pipeline só se ele ganhar.
4. Registre as quatro saídas intermediárias junto com cada resposta.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
