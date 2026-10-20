# background

[English](README.md) | **Português** | [Español](README.es-ES.md)

Memórias formadas "subconscientemente" depois que a conversa silencia: um extrator com debounce roda uma vez sobre a conversa inteira.

## O que faz

- `ReflectionExecutor` envolve um `StoreManager`. Cada turno chama `Submit` com 300 ms de atraso na mesma thread; todo submit cancela a execução pendente e reagenda.
- Três turnos rápidos disparam **uma** extração sobre a conversa inteira, gravando três memórias. Depois busca nelas de forma semântica.

## Como aplicar

```go
manager := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"}, langmem.WithStore(st))
executor := &langmem.ReflectionExecutor{Manager: manager}
fut, _ := executor.Submit(ctx, langmem.Payload{Messages: convo}, langmem.SubmitOptions{After: 30 * time.Second})
defer executor.Shutdown(true, false) // flush pending work on exit
```

Use para que a formação de memória nunca atrase uma resposta. Escolha o atraso pelo tempo que uma conversa fica parada antes de "acabar" (de segundos a minutos).

## Como rodar

```bash
go run ./examples/langmem/background
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo de extração real e um store durável; escolha `After` pelo ritmo real das suas conversas.
2. O timer é em processo: no encerramento chame `Shutdown(true, false)` para descarregar, e aceite que uma queda perde extrações pendentes (é um debounce, não uma fila). Para um executor remoto, veja `remote/server` e a lacuna indicada em LANGMEM.md.
3. Faça uma checagem de custo da extração: uma extração sobre a conversa inteira por período de silêncio, não por turno.
4. Revise a qualidade das memórias extraídas em conversas reais antes de habilitar para todos os usuários.

## Veja também

- [Manual, capítulo 16: Memória de longo prazo (LangMem)](../../../docs/pt-BR/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/pt-BR/LANGMEM.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
