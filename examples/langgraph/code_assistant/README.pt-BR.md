# code_assistant

[English](README.md) | **Português** | [Español](README.es-ES.md)

Gera código, checa, devolve o erro e tenta de novo até a checagem passar.

## O que faz

- `generate` pede ao modelo codificador; `check` valida o resultado (aqui um substituto em Go que exige `func Add(` e o comportamento certo).
- Em caso de falha o erro entra na conversa e o grafo volta, até 4 iterações (`graph.Sum()` as conta).
- O codificador roteirizado falha duas vezes (nome errado, operador errado) e acerta na terceira tentativa.

## Como aplicar

Troque o `check` substituto por um real: grave o código num módulo temporário, rode `go build ./... && go test ./...` com timeout e devolva a saída do compilador ou dos testes como erro.

```go
AddConditionalEdges("check", func(_ context.Context, s graph.State) (any, error) {
    if s["error"] != "" && s["iterations"].(int) < 4 { return "generate", nil }
    return graph.End, nil
})
```

## Como rodar

```bash
go run ./examples/langgraph/code_assistant
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um codificador real e um verificador real. Rode o verificador num sandbox isolado (contêiner ou `kubernetes.Sandbox`) sem rede, com limite de CPU/memória e timeout de relógio: você está executando código escrito por um modelo.
2. Mantenha o limite de iterações; registre cada tentativa e o erro que a disparou.
3. Trunque saídas longas do compilador antes de devolvê-las para que o contexto não se encha de ruído.
4. Nunca faça merge de código gerado sem revisão; use isto como gerador de rascunho.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
