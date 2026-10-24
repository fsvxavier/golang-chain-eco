# full_team_front

[English](README.md) | **Português** | [Español](README.es-ES.md)

Uma cópia de [full_team](../full_team/README.pt-BR.md) em que o time inteiro trabalha com front-end e experiência do usuário. Ele roda como servidor MCP: o VS Code (ou outro chat com MCP) envia uma demanda, e o time a transforma em código de telas revisado e em testes. A demanda passa pelo product owner, pelo time de engenharia, pelo desenvolvedor, pela revisão conjunta (especialista e tech lead) e pelo QA. Qualquer problema encontrado na revisão ou no QA volta para o desenvolvedor que escreveu o código.

Offline, roda com modelos falsos e um backend falso, então o fluxo inteiro funciona sem chave. Esta pasta é autocontida: o código do time foi copiado, não importado, porque o Go não permite importar o pacote `internal/` de outra árvore.

## O que faz

- **Time e modelos.** Doze workers (o `Cast` em `internal/team/local.go`), mais o manager e o hub. Neste exemplo tudo roda em um único processo: cada serviço escuta em sua própria porta de loopback e eles conversam por HTTP de verdade. Cada papel tem um modelo; os padrões valem quando só `ANTHROPIC_API_KEY` está definida.

  | Worker | Qtde | Modelo | Função |
  |---|---|---|---|
  | `po` (product owner) | 1 | `claude-opus-5-5` | lê o backend, escreve a spec |
  | `leader` (tech lead) | 1 | `claude-sonnet-5-5` | planeja as tarefas, indica um desenvolvedor preferido, participa da revisão conjunta |
  | `junior-1`, `junior-2` | 2 | `claude-haiku-5-5` | constroem o código das telas e seus testes unitários |
  | `mid-1`, `mid-2` | 2 | `claude-haiku-5-5` | idem |
  | `senior-1`, `senior-2` | 2 | `claude-haiku-5-5` | idem |
  | `specialist-1`, `specialist-2` | 2 | `claude-sonnet-5-5` | revisam o código com o tech lead |
  | `qa-1`, `qa-2` | 2 | `claude-sonnet-5-5` | escrevem os testes automatizados das telas |

  Mais dois papéis têm modelo mas não são workers: `manager` (`claude-sonnet-5-5`, conduz o fluxo de entrega e escreve o relatório) e `knowledge` (`claude-haiku-5-5`, responde às ferramentas de conhecimento no hub).

  Escolha do modelo, vale a primeira que casar, por papel: `AGENT_MODEL_<PAPEL>` (`MANAGER`, `PO`, `LEADER`, `JUNIOR`, `MID`, `SENIOR`, `SPECIALIST`, `QA`, `KNOWLEDGE`), depois `AGENT_MODEL` (um modelo para todos os papéis), depois `ANTHROPIC_API_KEY` (os padrões acima), depois `OPENAI_API_KEY` (`openai:gpt-4o-mini` para todos os papéis). Exemplos: `AGENT_MODEL_PO=anthropic:opus_5_5`, `AGENT_MODEL_QA=openai:gpt-4o-mini`. Sem nenhuma delas a execução é offline: todos os papéis respondem com um modelo roteirizado.

- **Fluxo.** O manager recebe a demanda e aciona o PO. O PO escreve a spec a partir da demanda e do backend que lê. O leader planeja as tarefas (`TASK|id|título|complexidade|descrição`, opcionalmente `|PREFER:<id do worker>`). O desenvolvedor constrói o código das telas e seus testes unitários. Em seguida roda a **revisão conjunta**: o especialista e o leader recebem a mesma entrega ao mesmo tempo, e a tarefa só passa quando os dois aprovam. Depois o QA escreve os testes automatizados das telas, rastreados até a spec do PO, e aprova quando eles a cobrem. As tarefas rodam em paralelo.

- **Quem recebe uma tarefa.** O leader é informado de quais desenvolvedores estão livres ou ocupados. O hub entrega o job ao primeiro worker livre da lista de preferência: o worker que o leader preferiu, depois os papéis por complexidade (`low`: junior, mid, senior; `medium`: mid, senior, junior; `high`: senior, mid, nunca um junior). Um job que ninguém pode assumir ainda espera na fila.

- **O retrabalho volta ao mesmo desenvolvedor.** Quando a revisão conjunta ou o QA rejeita uma tarefa, o feedback vai ao desenvolvedor que a escreveu, na mesma thread. Esse job de retrabalho é fixado (pinned): espera por aquele desenvolvedor em vez de ir a outro. Se o lease do desenvolvedor expirou, o job cai para um desenvolvedor livre do mesmo papel. Toda rejeição, da revisão ou do QA, custa uma rodada; uma tarefa rejeitada em `MaxRounds` (3) rodadas é registrada como `blocked` no relatório do manager e no histórico de handoffs (o status dela no tracker fica na última rejeição). A requisição ainda termina `completed`.

- **O PO lê o backend citado na demanda.** O PO tem três ferramentas somente de leitura, e só ele as tem. O PO não recebe ferramenta de shell; todos os outros workers têm uma. As raízes e as URLs vêm só dos campos estruturados de `team_submit` (`backend_roots`, `backend_urls`), nunca de texto livre do pedido. Uma demanda sem raízes e sem URLs não dá ao PO nenhum acesso: cada ferramenta recusa.
  - `backend_read`: lê um arquivo ou lista um diretório. O caminho pode ser relativo à primeira raiz ou absoluto dentro de uma raiz. Qualquer elemento `..` é recusado, e também um caminho real (depois de resolver symlinks) fora das raízes citadas. Toda raiz citada precisa estar dentro da raiz do workspace (`WORKSPACE_ROOT`, senão o diretório de trabalho); uma raiz relativa é tomada em relação à raiz do workspace, e uma raiz vazia não concede nada. A saída é limitada a 1 MB.
  - `backend_grep`: procura em um arquivo ou diretório sob as mesmas raízes com uma expressão regular de Go e devolve `caminho:linha: texto`. Nunca segue symlinks, para quando a saída chega a 1 MB e abre no máximo 5000 arquivos (`[truncated: stopped after 5000 files]`).
  - `backend_get`: apenas `GET` HTTP; não há argumento de método. O esquema deve ser http ou https, a URL não pode conter credenciais, e seu host e porta devem ser iguais aos de uma URL citada na demanda. Um redirect é verificado pela mesma regra (no máximo 5), então um redirect para outro host é recusado. Nenhum proxy é usado. O endereço realmente discado também é verificado: loopback, link-local (que contém o endereço de metadados `169.254.169.254`), não especificado e os endereços IPv6 de metadados da Alibaba e da AWS são recusados, a menos que esse `ip:porta` exato tenha sido citado como literal de IP. O nome `localhost`, quando a demanda o cita, pode alcançar endereços de loopback e nada além. Endereços privados (RFC 1918) são permitidos. O corpo é limitado a 1 MB e a chamada expira em 10 segundos.

  `WORKSPACE_ROOT` é lida pelas ferramentas de backend quando o servidor inicia; veja a seção de MCP para saber qual processo é esse.

- **A spec.** O PO lista os endpoints que leu, os payloads de requisição e resposta, os campos obrigatórios, os erros e os estados de UI (carregando, vazio, erro). Todo payload que descreve deve vir de algo que leu. A spec tem um bloco `EVIDENCE:` (arquivo e linha, ou URL, de cada payload) e um bloco `OPEN ITEMS:` para o que não conseguiu confirmar. O PO não bloqueia por itens em aberto e não inventa payload. **Regra de evidência:** se a demanda cita uma raiz ou URL de backend, a spec precisa de uma linha que comece com `EVIDENCE:` e de pelo menos uma chamada ao backend que passou pelas proteções e obteve resposta (chamadas recusadas não contam). Caso contrário o manager insere a linha `- UNVERIFIED: no backend evidence` sob `OPEN ITEMS:` (ou acrescenta um bloco `OPEN ITEMS:`). Uma demanda só com `app_url` não aciona a regra.

- **O QA escreve os testes.** O QA escreve um conjunto de testes por tarefa para as telas que os desenvolvedores construíram, a partir da spec do PO. Cada teste leva o nome do critério de aceitação ou do estado de UI que cobre, e uma linha `TRACE: AC1 -> <nome do teste>` por critério liga testes a critérios. O QA responde `VERDICT: APPROVE` ou `VERDICT: RETURN` com `FEEDBACK:` citando o item da spec que o código da tela não consegue satisfazer (um estado errado, um campo ausente, um problema de seletor ou de acessibilidade).

- **Execução opcional no navegador (`browser_test`).** Só o QA tem esta ferramenta. Ela roda a spec Playwright que o QA escreveu contra a `app_url` citada na demanda (o modelo não escolhe a URL). O comando é fixado pelo operador em `PLAYWRIGHT_CMD` (por exemplo `npx playwright test --project=chromium`), separado por espaços e executado sem shell, em um diretório temporário novo, com timeout de 2 minutos e saída limitada a 1 MB. A spec (no máximo 256 KB) é gravada em um arquivo e seu caminho é passado em `SPEC_FILE`; a URL é passada em `APP_URL`. O processo filho recebe um ambiente mínimo: `PATH`, um `HOME` e um `TMPDIR` dentro do diretório temporário, `CI=1`, `PLAYWRIGHT_BROWSERS_PATH` se definida, `APP_URL`, `SPEC_FILE`, e somente as variáveis que você listar em `PLAYWRIGHT_ENV_ALLOW` (separadas por vírgula). Chaves de provedor nunca chegam a ele, a menos que listadas. Em timeout ou cancelamento o grupo de processos inteiro é morto, e a espera pelos pipes é limitada a 5 segundos (somente Unix). `app_url` deve ser uma URL http ou https com host e sem caracteres de controle. Se `PLAYWRIGHT_CMD` estiver vazia, ou a demanda não tiver `app_url` válida, a ferramenta devolve `NOT RUN: <motivo>` e o QA ainda escreve os testes. Uma execução que falha é devolvida como texto `FAILED: ...`, não como erro da ferramenta. Como `HOME` é o diretório temporário, o Playwright precisa de `PLAYWRIGHT_BROWSERS_PATH` (definida ou listada em `PLAYWRIGHT_ENV_ALLOW`) para achar seus navegadores. O QA offline nunca chama a ferramenta.

- **Skills por persona.** Cada papel tem sua própria persona (front-end e UX) e skills de referência vendorizadas de repositórios públicos. Elas são embutidas com `go:embed`, então o exemplo roda em qualquer máquina. Veja [Skills vendorizadas](#skills-vendorizadas).

- **Compressão.** As chamadas de modelo de todo worker passam pela mesma camada de compressão do `full_team` (caveman nos turnos antigos, Ponytail, TOON, RTK para comandos de shell). Ela não é descrita de novo aqui: veja [full_team](../full_team/README.pt-BR.md#o-que-faz). A ferramenta de shell permite `go`, `git`, `ls`, `grep` e `echo` offline, e somente `ls`, `grep` e `echo` quando `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` está definida.

- **Offline.** O PO roteirizado chama `backend_read`, `backend_grep` e `backend_get` contra um backend falso embutido (um pequeno documento OpenAPI, uma árvore de handlers e um servidor `httptest`), então as proteções rodam de verdade, e a spec volta com um bloco `EVIDENCE:`. O leader roteirizado planeja cinco tarefas. Os revisores e o QA roteirizados rejeitam três delas uma vez, então o retorno ao mesmo desenvolvedor aparece: o tech lead rejeita `T2`, o especialista rejeita `T3`, o QA rejeita `T4`. O QA roteirizado responde só com linhas `VERDICT` e `TRACE:`; não escreve código de teste e não executa `browser_test`. A execução de demonstração falha se a spec offline voltar `UNVERIFIED`.

## Como aplicar

- **Escopo da demanda.** Passe o backend na demanda, não no texto do pedido: `backend_roots` para árvores de código, `backend_urls` para a API que o PO pode chamar, `app_url` para o front-end em execução que o QA pode abrir.
- **Papéis e modelos.** `anthropicDefaults` em `internal/team/models.go` e `Cast` em `internal/team/local.go` guardam os padrões e o pool. Altere-os ali e mantenha o pool de doze workers; `TestCastHasTwelveWorkers` e `TestDefaultModels` verificam isso.
- **Ferramentas de backend.** Os limites (saída de 1 MB, timeout de 10 segundos, 5000 arquivos de grep) estão em `internal/backend/backend.go`. Mantenha `WORKSPACE_ROOT` tão estreita quanto o backend permitir.
- **Skills.** Para mudar uma persona, edite seu `SKILL.md`. Para adicionar ou atualizar uma skill vendorizada, veja abaixo.
- **Shell.** A ferramenta de shell foi copiada do `full_team`. Com chave de provedor, o modelo só pode executar `ls`, `grep` e `echo`. Não adicione interpretadores nem `find`.

## Skills vendorizadas

`skills/skills.lock` tem 29 entradas, uma por cópia: cada pasta de persona tem sua própria cópia de toda skill que carrega, em `skills/<papel>/references/<nome>/SKILL.md`. O carregador as acrescenta ao prompt de sistema da persona, ordenadas por nome, sob `## Skill: <nome>`.

| Pasta da persona | Skills (cópias) | Tamanho |
|---|---|---|
| `product-owner` | `contract-first`, `openapi-spec-generation`, `user-stories` (3) | 9,1 KB |
| `engineering-manager` | `code-review`, `planning-and-task-breakdown` (2) | 5,9 KB |
| `tech-leader` | as mesmas duas (2) | 5,9 KB |
| `software-engineer-junior`, `-mid`, `-senior` | `frontend-design`, `frontend-patterns`, `react-testing`, `ui-ux-pro-max`, `vercel-react-best-practices` (5 cada, 15) | 17,3 KB cada |
| `specialist-reviewer` | `accessibility`, `frontend-code-review`, `performance-optimization`, `web-design-guidelines` (4) | 18,3 KB |
| `qa-engineer` | `playwright-best-practices`, `playwright-cli`, `webapp-testing` (3) | 10,7 KB |

São 17 skills distintas em 29 cópias. Fatos sobre elas:

- **São cópias reduzidas e saneadas, não os arquivos originais.** Exemplos e texto padrão foram cortados para caber no orçamento, e foram removidos comandos de instalação, instruções de busca remota, chamadas a scripts que não estão vendorizados e concessões de permissão de ferramentas (`allowed-tools`). Toda entrada do lock tem `"trimmed": true`.
- **Campos do lock.** Cada linha de `skills/skills.lock` é um objeto JSON: `role`, `source` (`dono/repo@skill`), `skill`, `installs` (a contagem de instalações que o `skills` mostrou, pode ser vazia), `hash` (SHA-256 do arquivo embutido), `license`, `trimmed` e, para os três snapshots, `pinned_from` (a URL do commit original).
- **Snapshots fixados.** `web-design-guidelines` é um snapshot fixado do `command.md` de vercel-labs/web-interface-guidelines no commit `434b7f9` (a skill original só buscava suas regras em uma URL em tempo de execução, e a cópia não busca nada). `playwright-best-practices` é uma cópia condensada de currents-dev/playwright-best-practices-skill em `283d5cb` (o original é um índice de muitos arquivos de referência). `user-stories` está fixada em phuryn/pm-skills em `07b34f6`.
- **Uma skill pedida não foi vendorizada.** `user-story` de deanpeters/product-manager-skills é CC BY-NC-SA 4.0 (uso não comercial, compartilhamento pela mesma licença), o que não serve para redistribuição aqui. O PO usa `user-stories` de phuryn/pm-skills (MIT) no lugar. Uma skill cuja licença não permite redistribuição é descartada, não parafraseada.
- **Licenças.** MIT na maioria; Apache-2.0 em `frontend-design`, `webapp-testing` e `playwright-cli`. `frontend-code-review` (especialista) está sob a Apache-2.0 modificada da Dify, com duas condições adicionais; elas estão citadas por inteiro nos avisos. [skills/THIRD_PARTY_NOTICES.md](skills/THIRD_PARTY_NOTICES.md) lista cada repositório de origem, skill, licença e titular de copyright como declarado na origem, com os textos das licenças, e também é embutido.
- **Fixadas.** Nada as atualiza automaticamente.

### Atualizar ou adicionar uma skill vendorizada

1. Busque a candidata em um diretório temporário, não na pasta global de skills: `npx skills add <dono/repo@skill> -y` executado dentro de um projeto temporário. Não use `-g`.
2. Leia o `SKILL.md` e a licença (o arquivo ou a licença do repositório). Se a licença não permite redistribuição, descarte.
3. Reduza e sanee até o orçamento: cerca de 8 KB por arquivo e 24 KB por papel. Remova comandos de instalação, buscas remotas, chamadas a scripts de arquivos que você não vendoriza e concessões `allowed-tools`. Copie o resultado para `skills/<papel>/references/<nome>/SKILL.md`, uma vez para cada persona que a carrega.
4. Acrescente ou atualize a entrada em `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (`sha256sum` do novo arquivo), `license`, `trimmed` (`true`) e `pinned_from` se for um snapshot.
5. Acrescente a licença, o repositório e o titular de copyright em `skills/THIRD_PARTY_NOTICES.md`.
6. Rode os testes abaixo. `skills/skills_test.go` verifica que toda entrada do lock tem seu arquivo embutido e o hash confere, que nenhum arquivo passa de 8 KB nem nenhum papel de 24 KB, que toda entrada tem licença e origem, que toda referência embutida está no lock e que os avisos citam toda licença, repositório e skill do lock.

## Como executar

Offline, um processo:

```bash
go run ./examples/langgraph/full_team_front
```

A execução imprime os serviços que iniciou e o relatório de entrega. Ela sobe o hub, o manager e os workers em loopback e os encerra no fim. Ela dá ao PO um backend falso em um workspace temporário; `WORKSPACE_ROOT` e `app_url` não se aplicam a ela.

Com um provedor real (os modelos são os padrões acima):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini para todos os papéis
```

## Usar como servidor MCP

O argumento `serve` roda o time inteiro no mesmo processo como servidor MCP via stdio (`serve.go`). Um editor o inicia como comando; não há mais nada para subir. Ele serve o time por meio de `Bridge.MCPServer` em `internal/team/bridge.go`.

A partir da raiz do repositório:

```bash
go run ./examples/langgraph/full_team_front serve
```

Ou compile um binário e execute (inicia mais rápido que `go run`, que compila a cada início):

```bash
go build -o bin/team-mcp ./examples/langgraph/full_team_front
bin/team-mcp serve
```

Na saída padrão o processo escreve apenas mensagens do protocolo MCP. Logs, as linhas de status do notificador de console e o relatório de compressão no encerramento vão para stderr. O servidor para ao fim da entrada (o editor fechando) ou com SIGINT/SIGTERM.

Modelos falsos offline são usados a menos que `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` esteja definida (ou `AGENT_MODEL*`, veja acima).

### Ambiente

Como o time roda no processo do servidor, toda variável vem do ambiente do servidor. No VS Code é o bloco `env` do `mcp.json`; no Claude Code é `--env` ou o shell.

| Variável | Lida por | Efeito |
|---|---|---|
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `AGENT_MODEL`, `AGENT_MODEL_<PAPEL>` | cada worker, o manager e o hub ao iniciar (`internal/team/models.go`) | modelos reais em vez dos roteirizados |
| `WORKSPACE_ROOT` | as ferramentas de backend do PO (`internal/backend/backend.go`) | toda entrada de `backend_roots` deve estar dentro dela; o padrão é o diretório de trabalho do servidor |
| `PLAYWRIGHT_CMD` | a ferramenta `browser_test` do QA, lida na partida (`main.go`) | comando que executa a spec; vazio significa `NOT RUN` |
| `PLAYWRIGHT_ENV_ALLOW` | o executor de comando do `browser_test` (`internal/qa/qa.go`) | variáveis do processo pai repassadas à execução no navegador |
| `NOTIFY`, `NOTIFY_STATUSES`, ... | o notificador de status do manager (`internal/tasktrack`) | para onde vão as linhas de status; `console` (stderr no modo serve) é o padrão |
| `BRIDGE_CIAM_CMD`, `BRIDGE_KNOWLEDGE_CMD` | a bridge | linhas de comando de servidores MCP extras cujas ferramentas são servidas junto às do time |
| `LAYA_URL`, `LAYA_API_KEY`, `LAYA_MODEL` | o decisor de compressão (`main.go`) | um decisor laya-serve real em vez do roteirizado |

`MANAGER_URL` e `HUB_URL` não são lidas no modo serve: a bridge está ligada aos serviços do próprio processo. Não defina `MANAGER_API_KEY` no modo serve: o manager passaria a exigi-la e a bridge do próprio processo não a envia.

### Ferramentas

O servidor se chama `engineering-team` e serve:

- `team_submit`: envia uma demanda. Seus campos são `request` e, opcionalmente, `backend_roots`, `backend_urls` e `app_url`. Devolve na hora `thread_id`, `run_id` e um status.
- `team_status`: para um `thread_id`, devolve `status` (a requisição e cada tarefa), `report`, `history` (os handoffs, incluindo cada rejeição de revisão e de QA e para quem voltou) e `running`. `status`, `report` e o `history` completo são preenchidos quando o último passo termina; enquanto a execução anda, o notificador de console imprime cada mudança de status em stderr.
- `team_queue`: os workers registrados (com o job atual) e os jobs na fila.
- `team_ask_worker`: faz uma pergunta a um worker, fora do fluxo de entrega. Um worker que responde assim não tem escopo de demanda, então as ferramentas de backend do PO recusam.
- `team_memory`: recupera o que o time registrou para uma requisição (argumentos `scope`, o `thread_id` da requisição, e `query`).
- `graph_run`, `graph_state`, `graph_resume`: ferramentas genéricas para qualquer grafo em um Agent Server (`url` tem como padrão o manager).
- As ferramentas dos servidores citados em `BRIDGE_CIAM_CMD` e `BRIDGE_KNOWLEDGE_CMD`, se definidas.

### Registrar

VS Code, `.vscode/mcp.json` (stdio; o formato do VS Code usa `servers`). Forma com `go run`, com o diretório de trabalho na raiz do repositório:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": { "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}" }
    }
  }
}
```

Forma com binário compilado:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "${workspaceFolder}/bin/team-mcp",
      "args": ["serve"],
      "env": {
        "WORKSPACE_ROOT": "${workspaceFolder}",
        "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}"
      }
    }
  }
}
```

Tire a linha `ANTHROPIC_API_KEY` para rodar offline. Depois abra o arquivo e clique em **Start** acima do servidor. No Copilot Chat escolha **Agent** e habilite `front-team` no seletor de ferramentas.

Claude Code:

```bash
claude mcp add --transport stdio front-team -- go run ./examples/langgraph/full_team_front serve
claude mcp add --transport stdio front-team -- /abs/path/bin/team-mcp serve
```

(Use uma das duas; acrescente `--env ANTHROPIC_API_KEY=...` ou `--env WORKSPACE_ROOT=...` antes do nome para passar variáveis.) Confira com `claude mcp list` ou `/mcp`.

Outros clientes MCP (Cursor, Kilo Code, Opencode) aceitam um comando stdio no mesmo formato (`command`, `args`, `env`); os trechos em [independent_engineering_team](../independent_engineering_team/README.pt-BR.md#4-registre-em-cada-aplicação) mostram o arquivo de cada cliente, com `command` e `args` trocados pelos acima. Eles não foram testados com este servidor.

### Uma demanda

Um exemplo de chamada a `team_submit` (os argumentos em JSON; os caminhos e URLs são marcadores). `backend_roots` deve estar dentro de `WORKSPACE_ROOT` (ou do diretório de trabalho do servidor):

```json
{
  "request": "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.",
  "backend_roots": ["/abs/path/to/workspace/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Depois chame `team_status` com o `thread_id` devolvido e repita até `report` estar presente e `running` estar vazio:

```json
{ "thread_id": "<thread_id from team_submit>" }
```

## Como testar

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
go vet ./examples/langgraph/full_team_front/...
```

Um teste: `go test -race -count=1 -run TestJointReviewNeedsBothApprovals ./examples/langgraph/full_team_front/internal/team`. Os testes, por pacote:

- `main` (`main_test.go`, `serve_test.go`): `TestAllowedProgramsWithProviderKey`, `TestScriptedPonytailFollowsRule`, `TestOfflineRunReportsTOON`, `TestPOGetsNoShell`; modo serve: `TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`, `TestServeStopsOnContextCancel`, `TestServeRestoresGlobals`. Os testes de serve também verificam que só o protocolo chega à saída padrão.
- `internal/team`: pool e fila (`TestReservationIsAtomic`, `TestPreferenceOrder`, `TestLeaseExpiryRequeues`, `TestJobFailsAfterMaxAttempts`, `TestCastHasTwelveWorkers`, `TestDefaultModels`, `TestNormalizeModel`); retrabalho fixado (`TestPinnedJobWaitsForItsWorker`, `TestPinnedJobFallsBackWhenLeaseExpired`, `TestRejectedWorkReturnsToSameDeveloper`, `TestTaskBlockedAfterMaxRounds`, `TestPlanParsesPreferField`); revisão conjunta (`TestJointReviewNeedsBothApprovals`); escopo de uma demanda (`TestScopeRoundTripOverHTTP`, `TestSubmitStoresScope`, `TestToolSeesJobScope`, `TestScopeNeverResolvedByPrefix`, `TestScopeIdCollision`); regra de evidência (`TestPOSpecWithoutEvidenceIsMarkedUnverified`, `TestVerifiedSpec`, `TestOfflineRunPOUsesBackendTools`); prompt do QA (`TestQAPromptCarriesSpec`, `TestQAOutputTracesCriteria`, `TestDossierDropsUnsafeAppURL`); bridge e time offline (`TestBridgeQueueAndAskUnknown`, `TestBridgeToolSetAndProxy`, `TestTeamDeliversOffline`, `TestStartLocalWithMiddleware`, `TestKnowledgeAndMemoryOverMCP`); carga das personas (`TestSkillIncludesReferences`, `TestSkillUnknownRole`, `TestNoArchitectInSkills`).
- `internal/backend`: `TestReadRefusesOutsideRoots`, `TestReadWithinRoot`, `TestGrepStaysInsideRoot`, `TestGrepLimits`, `TestRootsResolveAgainstWorkspace`, `TestGetRefusesNonGetAndUnnamedHost`, `TestGetRefusesRedirectToOtherHost`, `TestGetRefusesLoopbackUnlessNamed`, `TestGetNamedLocalhost`, `TestGetBodyCapped`, `TestNoScopeMeansNoAccess`, `TestCallsCountsGuardedCallsPerRequest`.
- `internal/qa`: `TestBrowserTestNotRunWithoutCommand`, `TestBrowserTestNotRunWithoutAppURL`, `TestBrowserTestRejectsNonHTTPAppURL`, `TestBrowserTestRunsRunnerWithScopeURL`, `TestCommandRunnerUsesNoShell`, `TestCommandRunnerCapsOutput`, `TestCommandRunnerMinimalEnv`, `TestCommandRunnerTimeoutKillsProcessGroup`, `TestToolNotGivenToOtherRoles`.
- `internal/tasktrack`: `TestTaskFlowAllowsRework`, `TestIllegalMoveIsRejectedAndNotRecorded`, `TestWebhookSinksAndFilter`, `TestNotifierFailureDoesNotBlock`, `TestFromEnvAndEmailMessage`.
- `skills`: `TestEveryLockEntryIsEmbedded`, `TestVendoredSkillWithinBudget`, `TestEveryLockEntryHasLicense`, `TestEveryRoleDirHasOwnCopies`, `TestNoticesCoverLockedLicensesAndSources`.

Os testes de serve que acionam o time offline (`TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`) são pulados quando algum papel tem especificação de modelo (uma chave ou `AGENT_MODEL*` está definida).

## Como testar em modo de produção

1. Registre o servidor (veja acima) com `ANTHROPIC_API_KEY` (ou `OPENAI_API_KEY`). No log do servidor em stderr, cada worker imprime `up on ... (model ...)`; confira se os modelos são os que você definiu (`AGENT_MODEL_<PAPEL>` sobrescreve os padrões).
2. Envie uma demanda pelo chat MCP com `team_submit`, com `backend_roots` e/ou `backend_urls` apontando para um backend real (e `app_url` para a execução no navegador). Consulte com `team_status` até `report` estar presente.
3. No `history` de `team_status`, procure uma rejeição da revisão conjunta (`review returned by ... -> <desenvolvedor>`) e uma rejeição do QA (`qa returned by ... -> <desenvolvedor>`). A linha seguinte daquela tarefa deve mostrar o mesmo desenvolvedor construindo de novo (`<tarefa>: <desenvolvedor> -> review`). Modelos nem sempre rejeitam; se nenhum rejeitou, envie uma demanda com uma lacuna proposital.
4. Confira a spec do PO: chame `graph_state` com o `thread_id` e leia `values.spec`. Ela deve conter um bloco `EVIDENCE:` em que cada endpoint corresponda ao backend para o qual você apontou, e os itens em aberto não devem conter `UNVERIFIED: no backend evidence`. Esse marcador significa que o PO não leu o backend.
5. Tente uma demanda que cita um host que o PO não deveria alcançar, e uma URL que redireciona para outro host. As duas devem ser recusadas na saída da ferramenta.
6. Sem `PLAYWRIGHT_CMD`, ou sem `app_url`, o resultado do `browser_test` do QA deve dizer `NOT RUN` com o motivo. Com as duas definidas (e `PLAYWRIGHT_BROWSERS_PATH`, e o Chrome instalado onde o servidor roda), deve mostrar a saída do Playwright.
7. Mantenha `WORKSPACE_ROOT` e as raízes de backend tão estreitas quanto o trabalho permitir: as ferramentas de backend leem arquivos no host que roda o servidor. Mantenha a lista de comandos permitidos do shell enxuta: com chave ela é `ls`, `grep` e `echo`, e a ferramenta de shell roda no host sem sandbox.

## Veja também

- [full_team](../full_team/README.pt-BR.md): o time copiado, com a camada de compressão documentada por completo
- [independent_engineering_team](../independent_engineering_team/README.pt-BR.md): o mesmo time dividido em serviços, com Docker compose, e os trechos de registro de cada cliente MCP
- [compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md), [compress_toon](../compress_toon/README.pt-BR.md), [compress_ponytail](../compress_ponytail/README.pt-BR.md)
