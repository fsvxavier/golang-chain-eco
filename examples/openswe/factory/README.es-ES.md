# factory

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Cómo llega el trabajo a los agents: un servidor de webhooks de GitHub, un planificador cron con automatizaciones almacenadas y un vigilante de CI.

## Qué hace

- Un `issue_comment` firmado que menciona al bot con @ inicia el thread de código de esa issue; un segundo comentario mientras corre se encola y se une a la misma ejecución.
- Los webhooks reenviados corren una vez; los extraños en otros repositorios y los comentarios del propio bot se ignoran; un PR nuevo en un repositorio con revisión automática habilitada inicia una review.
- Una automatización cron (`0 9 * * mon`) se dispara una vez por minuto coincidente (los claims evitan duplicados); un check de CI que falla despierta el thread del vigilante baby-sit exactamente una vez, y un job flaky de Actions se reejecuta dentro de un presupuesto.
- Los servicios de GitHub (y de Slack) son falsos en proceso de `examples/internal` (`fakegithub`, `fakeslack`), así que nada sale de tu máquina.

## Cómo aplicarlo

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: coder, triggers.KindReviewer: reviewer}, Queue: queue}
wh := &triggers.Webhook{Secret: secret, Dispatcher: d, Bot: "openswe", AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/github/webhook", wh)
```

El webhook verifica la firma, deduplica entregas (`Claims`), descarta eventos de bots y de fuera de la org y enruta al runner correcto. El `Dispatcher` encola mensajes para un thread ocupado.

## Cómo ejecutarlo

```bash
go run ./examples/openswe/factory
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Despliega el webhook detrás de TLS y configura la URL y el secreto del webhook en la GitHub App; comprueba que una petición sin firma se rechaza.
2. Cambia `MemoryClaims` y `guards.MemoryQueue` por versiones duraderas para que los reenvíos y los mensajes en cola sobrevivan a los reinicios y funcionen entre réplicas.
3. Ejecuta dos réplicas y envía la misma entrega a ambas: solo debe empezar una ejecución.
4. Habilita la revisión automática por repositorio (`review.Style.Enabled`) poco a poco; empieza con un único repo.
5. Define el presupuesto de reintentos del baby-sit (`babysit.MaxRetriesPerHead`) y alerta cuando se agote.

## Ver también

- [Manual, capítulo 19: Open SWE: una fábrica de software con agents](../../../docs/es-ES/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/es-ES/OPENSWE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
