# factory

[English](README.md) | **Português** | [Español](README.es-ES.md)

Como o trabalho chega aos agents: um servidor de webhooks do GitHub, um agendador cron com automações armazenadas e um vigia de CI.

## O que faz

- Um `issue_comment` assinado que menciona o bot com @ inicia a thread de código daquela issue; um segundo comentário enquanto ela roda entra na fila e se junta à mesma execução.
- Webhooks reenviados rodam uma vez; estranhos em outros repositórios e os comentários do próprio bot são ignorados; um PR novo num repositório com revisão automática habilitada inicia uma review.
- Uma automação cron (`0 9 * * mon`) dispara uma vez por minuto correspondente (claims evitam duplicatas); um check de CI que falha acorda a thread do vigia baby-sit exatamente uma vez, e um job flaky do Actions é reexecutado dentro de um orçamento.
- Os serviços do GitHub (e do Slack) são fakes em processo de `examples/internal` (`fakegithub`, `fakeslack`), então nada sai da sua máquina.

## Como aplicar

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: coder, triggers.KindReviewer: reviewer}, Queue: queue}
wh := &triggers.Webhook{Secret: secret, Dispatcher: d, Bot: "openswe", AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/github/webhook", wh)
```

O webhook verifica a assinatura, deduplica entregas (`Claims`), descarta eventos de bots e de fora da org e roteia para o runner certo. O `Dispatcher` enfileira mensagens para uma thread ocupada.

## Como rodar

```bash
go run ./examples/openswe/factory
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Implante o webhook atrás de TLS e configure a URL e o segredo do webhook no GitHub App; confira que uma requisição sem assinatura é rejeitada.
2. Troque `MemoryClaims` e `guards.MemoryQueue` por versões duráveis para que reenvios e mensagens na fila sobrevivam a reinícios e funcionem entre réplicas.
3. Rode duas réplicas e envie a mesma entrega a ambas: só uma execução deve começar.
4. Habilite a revisão automática por repositório (`review.Style.Enabled`) aos poucos; comece por um único repo.
5. Defina o orçamento de retries do baby-sit (`babysit.MaxRetriesPerHead`) e alerte quando ele se esgotar.

## Veja também

- [Manual, capítulo 19: Open SWE: uma fábrica de software com agents](../../../docs/pt-BR/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/pt-BR/OPENSWE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
