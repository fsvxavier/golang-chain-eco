# ralph_mode

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um loop de agents novos sem conversa compartilhada: o progresso passa só por um arquivo até a tarefa terminar.

## O que faz

- Cria `/progress.md` com três itens desmarcados.
- Cada iteração cria um agent **novo** e uma conversa nova, lê o arquivo, completa exatamente um item e o marca.
- O loop para quando o agent responde `ALL DONE`; o exemplo imprime o número de itens marcados por iteração.

## Como aplicar

Use para trabalho grande demais para uma única janela de contexto: cada passada começa limpa, então o contexto nunca estoura, e o arquivo é a fonte da verdade.

```go
for i := 0; i < maxIterations; i++ {
    agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: be})
    out, _ := agent.Invoke(ctx, graph.State{"messages": "Read /progress.md and complete exactly one unchecked item."})
    if strings.Contains(lastText(out), "ALL DONE") { break }
}
```

Escreva o arquivo de progresso de modo que qualquer agent novo consiga retomar: um item por linha, o status e um ponteiro para o artefato.

## Como rodar

```bash
go run ./examples/deepagents/ralph_mode
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use um modelo real e um diretório `backends.Filesystem` real, sob controle de versão.
2. Sempre limite as iterações (`maxIterations`) e adicione um timeout de relógio; um agent que nunca marca um item como feito ficaria em loop para sempre.
3. Verifique "feito" fora do modelo: rode os testes ou um checker após cada iteração e desfaça o item se falhar.
4. Faça commit a cada iteração para que uma passada ruim seja desfeita com um único revert.
5. Acompanhe os tokens por iteração; se o custo crescer, o arquivo de progresso está grande ou vago demais.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
