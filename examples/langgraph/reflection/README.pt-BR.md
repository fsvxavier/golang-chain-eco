# reflection

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um gerador e um crítico em loop até o crítico aprovar o rascunho.

## O que faz

- `generate` escreve uma descrição de uma frase; `reflect` a critica e devolve a crítica como mensagem humana; o loop termina em `APPROVED` ou quando a conversa passa de um limite de tamanho.
- O primeiro rascunho roteirizado é curto demais; o segundo passa.

## Como aplicar

```go
AddConditionalEdges("reflect", func(_ context.Context, s graph.State) (any, error) {
    if strings.Contains(demo.Last(s), "APPROVED") { return graph.End, nil }
    return "generate", nil
})
```

Use quando uma segunda passada melhora a saída de forma confiável (escrita, código, resumos). Dê ao crítico critérios concretos; um crítico vago aprova tudo ou implica para sempre.

## Como rodar

```bash
go run ./examples/langgraph/reflection
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos reais; um modelo diferente (muitas vezes mais forte) como crítico funciona melhor do que autocrítica.
2. Mantenha o limite de tamanho/iterações e meça com que frequência ele é atingido.
3. Compare custo com qualidade: amostre 20 tarefas com e sem reflexão e peça a uma pessoa ou a um eval que escolha a melhor.
4. Torne o sinal de aprovação estruturado em vez de casar uma string.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
