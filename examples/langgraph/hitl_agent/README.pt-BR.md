# hitl_agent

[English](README.md) | **Português** | [Español](README.es-ES.md)

Pausa uma chamada de tool para aprovação humana e depois retoma a mesma execução.

## O que faz

- `wire_money` tem portão em `middleware.HumanInTheLoop`. O primeiro `Invoke` retorna com um interrupt (`graph.Interrupts(out)`).
- Um `graph.Command{Resume: HITLResponse{Decisions: [Approve]}}` na mesma thread continua a execução e a tool roda.

## Como aplicar

O menor fluxo de aprovação. Use como modelo para qualquer ação irreversível: pagamentos, e-mails, exclusões, deploys.

```go
ctx := runnables.WithConfig(ctx, graph.Thread("demo"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Wire $500 to Bob"})
// show graph.Interrupts(out) to a human ...
out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{{Type: middleware.Approve}}}})
```

## Como rodar

```bash
go run ./examples/langgraph/hitl_agent
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Troque `checkpoint.NewMemory()` por um saver durável e entregue o pedido de aprovação pelo seu canal real (UI, Slack, ticket).
2. Adicione uma política de timeout/expiração: um pedido que ninguém responde não deve executar depois, de surpresa.
3. Permita `Edit` só onde editar argumentos for seguro, e revalide os argumentos editados.
4. Registre em log de auditoria o pedido, a decisão, o aprovador e o resultado.
5. Teste a retomada após um reinício do processo para provar que a pausa é realmente durável.

## Veja também

- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
