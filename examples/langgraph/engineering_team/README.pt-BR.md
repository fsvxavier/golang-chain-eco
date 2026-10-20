# engineering_team

[English](README.md) | **Português** | [Español](README.es-ES.md)

Uma organização de engenharia como um único grafo: um gerente roteia o pedido, um product owner o especifica, um arquiteto avalia o risco, um tech leader o divide em tarefas e quatro engenheiros constroem em paralelo; cada tarefa passa por code review e QA antes da entrega. Todos os agents rodam em um só processo.

## O que faz

O elenco, cada um com a persona em `skills/<nome>/SKILL.md`:

| Papel | Qtd | Função |
|---|---|---|
| Software Engineer Manager | 1 | Recebe o pedido, roteia e escreve o relatório final. Nunca escreve código (o nó falha se a resposta tiver um bloco de código). |
| Product Owner | 1 | Transforma o pedido numa spec com critérios de aceite numerados. |
| Arquiteto | 1 | Classifica o risco e define restrições e regras de segurança que acompanham cada tarefa. |
| Tech Leader | 1 | Divide a spec em tarefas e classifica cada uma como complexidade `low`, `medium` ou `high`. |
| Software Engineers | 4 | 2 juniores, 1 pleno e 1 sênior; implementam em paralelo. |
| Revisores especialistas | 2 | Aprovam uma tarefa ou a devolvem com feedback. |
| Engenheiros de QA | 2 | Aprovam uma tarefa ou a devolvem com um defeito reproduzível. |

Fluxo: `manager -> po -> architect -> leader -> (dev -> review -> qa -> done) x N -> deliver`. Uma tarefa devolvida pela review ou pelo QA volta ao mesmo engenheiro com o feedback; depois de 3 devoluções ela é reportada como `blocked`.

Três mecanismos fazem funcionar:

- **Reserva atômica.** O registro reserva pessoas sob um único lock, então duas tarefas nunca recebem o mesmo engenheiro, revisor ou QA. A atribuição do líder prefere o encaixe pela complexidade (`high` nunca vai para júnior); quando ninguém adequado está livre, a tarefa espera num backlog e é atribuída quando alguém é liberado. O plano da demo tem cinco tarefas para quatro engenheiros.
- **O contexto viaja com o trabalho.** Cada tarefa carrega o dossiê inteiro (pedido, spec, avaliação do arquiteto) e o feedback recebido, porque o argumento de um `Send` é tudo o que um nó enxerga. O estado compartilhado guarda o histórico de handoffs numa thread com checkpoint (`graph.Thread("coupons")`).
- **Um nó `deliver` adiado** roda uma vez quando todas as tarefas saíram do pipeline, e então o gerente escreve o relatório.

## Como aplicar

- Edite as personas: cada `skills/<nome>/SKILL.md` é o system prompt do agent correspondente (o código Go embute a pasta com `go:embed` e remove o frontmatter). Cada skill define o formato de saída que o código interpreta (`TASK|id|título|complexidade|descrição`, `VERDICT: APPROVE|RETURN`, `RISK:`, ...); mude os formatos na skill e em `main.go` juntos.
- Mude o time: adicione membros em `newRegistry` e uma skill para qualquer papel novo.
- Mude o pipeline: os nós são funções simples em `main.go`; o roteamento é `Command{Send}`.
- O registro é um mutex local ao processo. Para agents em processos separados seria preciso um store com compare-and-swap ou lease, mais uma fila durável (veja `remote/` e `agentprotocol/`).

## Como rodar

```bash
go run ./examples/langgraph/engineering_team
go test -race ./examples/langgraph/engineering_team   # testes de atomicidade do registro e do backlog
```

Offline, um modelo roteirizado (`brain.go`) responde pela linha `ROLE:` de cada prompt e faz a review devolver T3 e o QA devolver T2 uma vez, para que o retorno ao engenheiro fique visível. Com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY`, o mesmo grafo roda com um modelo real (um modelo para o time inteiro; a persona vem de cada skill). Espere que as linhas paralelas se intercalem de forma diferente a cada execução.

## Status das tarefas e notificações

Toda tarefa (e o pedido) tem sempre um de 13 status: `received`, `in_specification`, `risk_sec_review`, `task_assignment`, `in_development`, `in_code_review`, `in_qa_testing`, `qa_approved`, `qa_rejected`, `review_approved`, `review_rejected`, `committed`, `completed`. O agente que está com o trabalho é registrado em cada movimentação.

```
request: received -> in_specification -> risk_sec_review -> task_assignment -> completed
task:    task_assignment -> in_development -> in_code_review -> review_approved -> in_qa_testing -> qa_approved -> committed -> completed
         in_code_review -> review_rejected -> in_development      in_qa_testing -> qa_rejected -> in_development
```

`examples/internal/tasktrack` valida cada movimentação contra esta tabela (uma tarefa não chega a `committed` sem revisão e QA; movimentação ilegal é logada e não registrada). Uma tarefa que esgota as 3 rodadas fica na rejeição que a parou. Aqui `committed` significa que o código aprovado foi registrado como entrega da tarefa; uma execução real commita antes no sandbox.

Cada movimentação é anunciada por `NOTIFY` (padrão `console`): `console`, `slack` (`SLACK_WEBHOOK_URL`), `email` (`SMTP_ADDR`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `MAIL_TO`) e `chat` (`CHAT_WEBHOOK_URL`, qualquer webhook que aceite `{"text": "...", "event": {...}}`), separados por vírgula. `NOTIFY_STATUSES=review_rejected,qa_rejected,completed` limita slack, email e chat a esses status; o console mostra todos. Um canal que falha é logado e nunca trava o trabalho.

```bash
NOTIFY=console,slack SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... NOTIFY_STATUSES=qa_rejected,completed go run ./examples/langgraph/engineering_team
```

## Como testar em modo produtivo

1. Defina uma chave de provedor e rode de novo. A estrutura deve se manter (as cinco tarefas chegam a `approved` ou `blocked`), mas a redação e o número de rodadas de review vão variar. Se a execução falhar por erro de parsing, um modelo ignorou o formato de saída de uma skill: aperte essa skill.
2. Faça a checagem ser mais rígida que o modelo: o código devolvido por um engenheiro é texto. Em produção, faça o nó do engenheiro gravá-lo num sandbox e rodar o build e os testes reais antes da review (veja `examples/integrations/remote_sandbox` e `kubernetes.Sandbox`).
3. Troque `checkpoint.NewMemory()` por um saver durável (`sqlsaver`, Redis, ...) e derive o `thread_id` do id do seu item de trabalho; mate o processo no meio de uma execução e retome.
4. Limite o custo: o loop tem `maxRounds`; defina também `ModelCallLimit`/timeouts por agent e um `RecursionLimit` no grafo.
5. Se os agents precisarem viver em serviços separados, coloque cada um atrás de `agentprotocol`/`remote.Graph` e troque o registro em processo por um store de reservas compartilhado.
6. Avalie as personas como código: rode um conjunto de pedidos (`examples/evals/basic`) e compare planos, taxa de defeitos pegos pela review e defeitos do QA antes e depois de editar uma skill.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
- `examples/langgraph/multi_agent_supervisor` e `hierarchical_teams`: formatos de supervisor mais simples
