# customer_support

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um assistente cujas tools somente-leitura rodam livremente, mas cuja tool sensível exige aprovação humana.

## O que faz

- `lookup_booking` é livre; `change_booking` é configurada em `middleware.HumanInTheLoop` com as decisões permitidas `Approve` e `Reject`.
- O modelo pede para consultar e alterar uma reserva; a execução pausa com um interrupt. O exemplo roda duas threads: uma aprova (a reserva muda) e outra rejeita (a reserva fica intacta).

## Como aplicar

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{
    "change_booking": {Allowed: []string{middleware.Approve, middleware.Reject}},
}}
// pause: out := agent.Invoke(ctx, input); req := graph.Interrupts(out)[0].Value.(middleware.HITLRequest)
// resume: agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: ...}})
```

O mesmo `thread_id` precisa ser usado para retomar. Classifique as tools pelo efeito colateral: leituras livres, escritas e pagamentos com portão.

## Como rodar

```bash
go run ./examples/langgraph/customer_support
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um checkpointer **durável** (`sqlsaver`, Redis, ...) em vez de `checkpoint.NewMemory()`: uma aprovação pode chegar horas depois, após um reinício.
2. Derive o `thread_id` da sessão autenticada, nunca de entrada do cliente.
3. Monte a interface de aprovação a partir de `HITLRequest` (nome e argumentos da tool) e registre quem aprovou o quê e quando.
4. Torne `change_booking` idempotente e revalide as permissões na execução, não só na aprovação.
5. Teste o caminho de rejeição: o agent deve dizer ao cliente que nada mudou.

## Veja também

- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
