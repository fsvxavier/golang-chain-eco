# rewoo

[English](README.md) | **Português** | [Español](README.es-ES.md)

Reasoning WithOut Observation: planeja todas as chamadas de tool de antemão com placeholders (`#E1`, `#E2`), executa em ordem e depois resolve.

## O que faz

- O planner emite pares `Plan:` / `#E1 = Search[...]` de uma vez; uma regexp os converte em passos.
- `tool` roda um passo por vez, substituindo evidências anteriores (`#E1`) nas entradas seguintes, e junta os resultados com um reducer `BinOp`; `solve` escreve a resposta final a partir das evidências.

## Como aplicar

Em comparação com ReAct, o modelo é chamado duas vezes (planejar e resolver) em vez de uma vez por chamada de tool, o que economiza tokens quando o plano não depende de surpresas intermediárias.

```go
var stepRe = regexp.MustCompile(`Plan:\s*(.+)\n\s*(#E\d+)\s*=\s*(\w+)\[(.+)\]`)
```

Valide o plano interpretado (tools conhecidas, variáveis definidas antes do uso) antes de rodar qualquer coisa.

## Como rodar

```bash
go run ./examples/langgraph/rewoo
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos e tools reais; a saída do modelo vai se afastar do formato exato, então aperte o prompt e rejeite planos que não interpretam.
2. Limite o número de passos e defina timeouts por tool.
3. Nunca substitua evidências num comando de shell ou numa query sem escapar; trate resultados de tools como não confiáveis.
4. Compare tokens e acurácia com um agent ReAct nas mesmas tarefas.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
