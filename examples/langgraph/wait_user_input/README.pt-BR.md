# wait_user_input

[English](README.md) | **Português** | [Español](README.es-ES.md)

Pausa um grafo para perguntar algo ao usuário e depois retoma com a resposta.

## O que faz

- `ask_location` chama `graph.Interrupt(ctx, "Where are you?")`; o primeiro `Invoke` retorna com o interrupt e `GetState` mostra o próximo nó.
- `Invoke(ctx, graph.Command{Resume: "Lisbon"})` na mesma thread continua e `report` usa a resposta.

## Como aplicar

```go
loc := graph.Interrupt(ctx, "Where are you?") // returns the Resume value after the pause
app.Invoke(ctx, graph.Command{Resume: "Lisbon"})
```

Nada fica rodando enquanto espera: o estado está no checkpointer. Um handler web pode devolver a pergunta ao cliente e retomar quando a resposta chegar.

## Como rodar

```bash
go run ./examples/langgraph/wait_user_input
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Use um checkpointer durável; com `NewMemory` um reinício perde a pergunta pendente.
2. Expire threads antigas (`DeleteThread`) e autentique a requisição de retomada contra o dono da thread.
3. Valide a resposta antes de retomar.
4. Teste retomada dupla (a mesma resposta enviada duas vezes) e retomada de uma thread desconhecida.

## Veja também

- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
