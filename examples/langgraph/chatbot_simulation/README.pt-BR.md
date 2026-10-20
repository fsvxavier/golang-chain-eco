# chatbot_simulation

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um usuário simulado conversa com o seu assistente e um avaliador pontua a conversa.

## O que faz

- Três papéis num grafo: `assistant`, `simulated_user` (um modelo que faz o papel do cliente, com os papéis invertidos) e `evaluate` (um avaliador que devolve JSON com uma nota).
- O loop termina quando o usuário simulado diz `FINISHED` ou a conversa fica longa.
- Imprime cada turno e o veredito final.

## Como aplicar

Use como harness de regressão de um chatbot: escreva personas e objetivos para o usuário simulado, rode muitas conversas e compare as notas antes e depois de uma mudança de prompt ou modelo.

```go
out, _ := grader.Generate(ctx, []messages.Message{messages.Human("Score this support chat 1-5 as JSON:\n" + messages.Buffer(ms))})
```

## Como rodar

```bash
go run ./examples/langgraph/chatbot_simulation
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Troque o assistente pelo seu agent real (o mesmo que você implanta) e use modelos reais para o usuário e o avaliador.
2. Monte 20+ cenários (caminho feliz, cliente irritado, fora de assunto, adversarial) e rode cada um várias vezes: simulações são estocásticas.
3. Faça o avaliador devolver critérios estruturados, não uma nota só, e confira as notas dele contra avaliações humanas por amostragem.
4. Rode no CI a cada mudança de prompt ou modelo e quebre o build se a nota cair; veja também `examples/evals/basic`.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 12: Observabilidade](../../../docs/pt-BR/manual/12-observabilidade.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
