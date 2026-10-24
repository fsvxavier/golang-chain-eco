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

- **Fluxo.** O manager recebe a demanda e aciona o PO. O PO escreve a spec a partir da demanda e do backend que lê. O leader planeja as tarefas (`TASK|id|título|complexidade|descrição`, opcionalmente `|PREFER:<id do worker>`). O desenvolvedor constrói o código das telas e seus testes unitários. Em seguida roda a **revisão conjunta**: o especialista e o leader recebem a mesma entrega ao mesmo tempo, e a tarefa só passa quando os dois aprovam. Depois o QA escreve os testes automatizados das telas, rastreados até a spec do PO, e aprova quando eles a cobrem. As tarefas rodam em paralelo. O `PREFER:` do leader só é mantido se nomeia um id de worker do elenco cujo papel serve à complexidade da tarefa (um nome de papel sozinho, um id desconhecido, um revisor ou um junior em tarefa `high` é descartado, com uma linha de log); uma complexidade desconhecida vira `medium`.

- **Quem recebe uma tarefa.** O leader é informado de quais desenvolvedores estão livres ou ocupados. O hub entrega o job ao primeiro worker livre da lista de preferência: o worker que o leader preferiu, depois os papéis por complexidade (`low`: junior, mid, senior; `medium`: mid, senior, junior; `high`: senior, mid, nunca um junior). Um job que ninguém pode assumir ainda espera na fila.

- **O retrabalho volta ao mesmo desenvolvedor.** Quando a revisão conjunta ou o QA rejeita uma tarefa, o feedback vai ao desenvolvedor que a escreveu, na mesma thread. Esse job de retrabalho é fixado (pinned): espera por aquele desenvolvedor em vez de ir a outro. Se o lease do desenvolvedor expirou, o job cai para um desenvolvedor livre do mesmo papel. Toda rejeição, da revisão ou do QA, custa uma rodada; uma tarefa rejeitada em `MaxRounds` (3) rodadas é registrada como `blocked` no relatório do manager e no histórico de handoffs (o status dela no tracker fica na última rejeição). A requisição ainda termina `completed`.

- **O PO lê o backend citado na demanda.** O PO tem três ferramentas somente de leitura, e só ele as tem. O PO nunca tem ferramenta de shell (veja Shell em Como aplicar). As raízes e as URLs vêm só dos campos estruturados de `team_submit` (`backend_roots`, `backend_urls`), nunca de texto livre do pedido. Uma demanda sem raízes e sem URLs não dá ao PO nenhum acesso: cada ferramenta recusa.
  - `backend_read`: lê um arquivo ou lista um diretório. O caminho pode ser relativo à primeira raiz ou absoluto dentro de uma raiz. Qualquer elemento `..` é recusado, e também um caminho real (depois de resolver symlinks) fora das raízes citadas. Toda raiz citada precisa estar dentro da raiz do workspace (`WORKSPACE_ROOT`, senão o diretório de trabalho); uma raiz relativa é tomada em relação à raiz do workspace, e uma raiz vazia não concede nada. A saída é limitada a 1 MB.
  - `backend_grep`: procura em um arquivo ou diretório sob as mesmas raízes com uma expressão regular de Go e devolve `caminho:linha: texto`. Nunca segue symlinks, para quando a saída chega a 1 MB e abre no máximo 5000 arquivos (`[truncated: stopped after 5000 files]`).
  - `backend_get`: apenas `GET` HTTP; não há argumento de método. O esquema deve ser http ou https, a URL não pode conter credenciais, e seu host e porta devem ser iguais aos de uma URL citada na demanda. Um redirect é verificado pela mesma regra (no máximo 5), então um redirect para outro host é recusado. Nenhum proxy é usado. O endereço realmente discado também é verificado: loopback, link-local (que contém o endereço de metadados `169.254.169.254`), não especificado e os endereços IPv6 de metadados da Alibaba e da AWS são recusados, a menos que esse `ip:porta` exato tenha sido citado como literal de IP. O nome `localhost`, quando a demanda o cita, pode alcançar endereços de loopback e nada além. Endereços privados (RFC 1918) são permitidos. O corpo é limitado a 1 MB e a chamada expira em 10 segundos.

  `WORKSPACE_ROOT` é lida pelas ferramentas de backend quando o servidor inicia; veja a seção de MCP para saber qual processo é esse.

- **A spec.** O PO lista os endpoints que leu, os payloads de requisição e resposta, os campos obrigatórios, os erros e os estados de UI (carregando, vazio, erro). Todo payload que descreve deve vir de algo que leu. A spec tem um bloco `EVIDENCE:` (arquivo e linha, ou URL, de cada payload) e um bloco `OPEN ITEMS:` para o que não conseguiu confirmar. O PO não bloqueia por itens em aberto e não inventa payload. **Regra de evidência:** se a demanda cita uma raiz ou URL de backend, a spec precisa de uma linha que comece com `EVIDENCE:` e de pelo menos uma chamada ao backend que passou pelas proteções e obteve resposta (chamadas recusadas não contam). A contagem fica no hub (`POST /calls/{id}`, `GET /calls/{id}`): as ferramentas do PO reportam ali cada chamada assim, sob o id da requisição, e o manager lê a contagem, então a regra vale quando o manager e o PO rodam como serviços separados. O hub zera a contagem de uma requisição quando o escopo dela é gravado na entrada, então uma nova execução da mesma thread começa do zero. Caso contrário o manager insere a linha `- UNVERIFIED: no backend evidence` sob `OPEN ITEMS:` (ou acrescenta um bloco `OPEN ITEMS:`). Uma demanda só com `app_url` não aciona a regra.

- **O QA escreve os testes.** O QA escreve um conjunto de testes por tarefa para as telas que os desenvolvedores construíram, a partir da spec do PO. Cada teste leva o nome do critério de aceitação ou do estado de UI que cobre, e uma linha `TRACE: AC1 -> <nome do teste>` por critério liga testes a critérios. O QA responde `VERDICT: APPROVE` ou `VERDICT: RETURN` com `FEEDBACK:` citando o item da spec que o código da tela não consegue satisfazer (um estado errado, um campo ausente, um problema de seletor ou de acessibilidade).

- **Execução opcional no navegador (`browser_test`).** Só o QA tem esta ferramenta. Ela roda a spec Playwright que o QA escreveu contra a `app_url` citada na demanda (o modelo não escolhe a URL). O comando é fixado pelo operador em `PLAYWRIGHT_CMD` separado por espaços e executado sem shell, em um diretório temporário novo e vazio, com timeout de 2 minutos e saída limitada a 1 MB. A spec (no máximo 256 KB) é gravada em um arquivo e seu caminho é passado em `SPEC_FILE`; a URL é passada em `APP_URL`. O processo filho recebe um ambiente mínimo: `PATH`, um `HOME` e um `TMPDIR` dentro do diretório temporário, `CI=1`, `PLAYWRIGHT_BROWSERS_PATH` se definida, `APP_URL`, `SPEC_FILE`, e somente as variáveis que você listar em `PLAYWRIGHT_ENV_ALLOW` (separadas por vírgula). Chaves de provedor nunca chegam a ele, a menos que listadas. Em timeout ou cancelamento o grupo de processos inteiro é morto, e a espera pelos pipes é limitada a 5 segundos (somente Unix). `app_url` deve ser uma URL http ou https com host e sem caracteres de controle. Se `PLAYWRIGHT_CMD` estiver vazia, ou a demanda não tiver `app_url` válida, a ferramenta devolve `NOT RUN: <motivo>` e o QA ainda escreve os testes. Uma execução que falha é devolvida como texto `FAILED: ...`, não como erro da ferramenta. O processo filho, portanto, não tem configuração do Playwright nem `node_modules`, e seu `HOME` é o diretório temporário, então um `npx playwright test` simples não tem o que executar ali (não testado). Recomendado: aponte `PLAYWRIGHT_CMD` para um script wrapper que viva no seu próprio projeto Playwright; o script lê `APP_URL` e `SPEC_FILE` do ambiente, copia a spec para o projeto, entra nele com `cd` e executa o Playwright ali (não testado aqui). O Playwright precisa de `PLAYWRIGHT_BROWSERS_PATH` (definida, ou listada em `PLAYWRIGHT_ENV_ALLOW`) para achar seus navegadores, e qualquer outra variável de que o script precise deve ser listada em `PLAYWRIGHT_ENV_ALLOW`. O QA offline nunca chama a ferramenta.

- **Skills por persona.** Cada papel tem sua própria persona (front-end e UX) e skills de referência vendorizadas de repositórios públicos. Elas são embutidas com `go:embed`, então o exemplo roda em qualquer máquina. Veja [Skills vendorizadas](#skills-vendorizadas).

- **Compressão.** As chamadas de modelo de todo worker passam pela mesma camada de compressão do `full_team` (caveman nos turnos antigos, Ponytail, TOON, RTK para comandos de shell). Ela não é descrita de novo aqui: veja [full_team](../full_team/README.pt-BR.md#o-que-faz). A ferramenta de shell existe só na execução de demonstração; veja Shell em Como aplicar.

- **Offline.** O PO roteirizado chama `backend_read`, `backend_grep` e `backend_get` contra um backend falso embutido (um pequeno documento OpenAPI, uma árvore de handlers e um servidor `httptest`), então as proteções rodam de verdade, e a spec volta com um bloco `EVIDENCE:`. O cenário é uma tela de front-end, um campo de cupom no formulário de checkout. O leader roteirizado planeja cinco tarefas: `T1` componente do campo de cupom, `T2` hook de validação, `T3` anúncio de erro e proteção contra duplo envio (preferindo `senior-1`), `T4` linha de desconto e estados de UI, `T5` teclado e foco; os desenvolvedores roteirizados respondem com um bloco TSX. Os revisores e o QA roteirizados rejeitam três delas uma vez, então o retorno ao mesmo desenvolvedor aparece: o tech lead rejeita `T2`, o especialista rejeita `T3`, o QA rejeita `T4`. Offline os workers também têm uma ferramenta `open_tasks` (um backlog fixo que dá à camada de compressão um resultado de ferramenta para reescrever); ela só é registrada quando nenhum papel roda em um modelo real. O QA roteirizado responde só com linhas `VERDICT` e `TRACE:`; não escreve código de teste e não executa `browser_test`. A execução de demonstração falha se a spec offline voltar `UNVERIFIED`.

## Como aplicar

- **Escopo da demanda.** Passe o backend na demanda, não no texto do pedido: `backend_roots` para árvores de código, `backend_urls` para a API que o PO pode chamar, `app_url` para o front-end em execução que o QA pode abrir.
- **Papéis e modelos.** `anthropicDefaults` em `internal/team/models.go` e `Cast` em `internal/team/local.go` guardam os padrões e o pool. Altere-os ali e mantenha o pool de doze workers; `TestCastHasTwelveWorkers` e `TestDefaultModels` verificam isso.
- **Ferramentas de backend.** Os limites (saída de 1 MB, timeout de 10 segundos, 5000 arquivos de grep) estão em `internal/backend/backend.go`. Mantenha `WORKSPACE_ROOT` tão estreita quanto o backend permitir.
- **Skills.** Para mudar uma persona, edite seu `SKILL.md`. Para adicionar ou atualizar uma skill vendorizada, veja abaixo.
- **Shell.** A ferramenta de shell foi copiada do `full_team` e existe só na execução de demonstração (`go run` sem `serve`): o modo `serve` não dá shell a nenhum papel, então a saída dos desenvolvedores é texto. Na demonstração o processo filho do shell vê apenas `PATH` e um `HOME` em um diretório temporário (sem chaves de provedor), e um comando que contenha `<`, `>`, `$`, crase, barra invertida ou chave é rejeitado. Offline a lista de permitidos é `go`, `git`, `ls`, `grep` e `echo`; `go` e `git` saem sempre que algum papel tem um modelo real (uma chave de provedor, `AGENT_MODEL` ou `AGENT_MODEL_<PAPEL>`), restando `ls`, `grep` e `echo`. Não adicione interpretadores nem `find`. Os argumentos de caminho ficam confinados ao diretório de trabalho do shell. O comando é separado em tokens uma só vez, respeitando aspas simples e duplas, e dividido em segmentos só em `;`, `&`, `|` e quebra de linha fora de aspas; todo token de todo segmento é verificado. Um nome simples, um argumento de caminho, o valor após `=` em um `--opção=valor` ou palavra `VAR=valor`, e o texto colado a uma opção curta (`-fCAMINHO`) que nomeie um caminho existente fora do diretório (caminho absoluto, `..` ou link simbólico) são rejeitados com `shell: paths outside the workspace are not allowed: <token>`, assim como qualquer coisa que comece com `~`. Um glob (`*`, `?`, `[`) é verificado de dois modos. Um glob com cara de caminho (tem `/`, ou começa com `.` ou `~`) é rejeitado quando contém `[`, quando o prefixo literal (os componentes antes do primeiro componente com curinga) não está dentro do diretório (então `/etc/pass*`, `../*/` e `/*/` são rejeitados sem olhar as correspondências), ou quando um componente `..` vem depois de um curinga. Qualquer glob também é rejeitado quando uma correspondência dele, ou da forma limpa (`/` final, `/.`, `//`), está fora do diretório; isso vale também dentro de aspas, o que é mais estrito que o bash. Para globs simples, e para qualquer glob com `[`, a verificação de correspondências roda também numa cópia em que cada expressão de colchetes vira `?` (casa exatamente um caractere no bash e no Go, então a cópia casa um superconjunto), porque os dois dialetos leem `[!x]`, `[]x]` e `[[:alpha:]]` de forma diferente. Um componente com curinga que começa com `.` e pode casar `.` ou `..` (`.*`, `.?`, `..*`) é recusado, porque o bash anterior ao 5.2 o expande para `..`; isso recusa também uma regex simples de grep como `'.*'`. Um padrão simples (`grep '[0-9]' file`) e um glob do workspace (`ls *`, `src/*.tsx`) funcionam, e um glob sem correspondência passa. Todo token depois de um `--` isolado conta como caminho. Um token que não existe em disco (um padrão de grep como `/api/coupon`) passa. Barras invertidas e chaves são rejeitadas de saída. Opções que leem arquivos são negadas: no `grep`, `-f`, `--file`, `--exclude-from`, `--include-from`, `-R` e `--dereference-recursive`, também como qualquer prefixo longo não ambíguo (`--deref`) e dentro de grupos curtos como `-nf` (então `grep -efoo` também é recusado; `-r`, `-n` e `--include=*.go` continuam funcionando); no `go` e no `git`, `--git-dir`, `--work-tree` e um `-C` antes do subcomando. As demais opções não são inspecionadas. É uma proteção para uma ferramenta só de demonstração, não um sandbox.

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

`MANAGER_URL` e `HUB_URL` não são lidas no modo serve: a bridge está ligada aos serviços do próprio processo. No modo serve o manager é protegido por um `x-api-key` aleatório por execução (32 bytes aleatórios, em hexadecimal) que só a bridge do próprio processo conhece; uma `MANAGER_API_KEY` definida pelo operador é ignorada. O hub e os servidores Agent Protocol dos workers escutam em `127.0.0.1` e não têm autenticação: qualquer processo local pode alcançá-los. Cancelar durante a partida (SIGINT, SIGTERM, o editor fechando) encerra o processo de imediato e sem erro.

### Ferramentas

O servidor se chama `engineering-team` e serve:

- `team_submit`: envia uma demanda. Seus campos são `request` e, opcionalmente, `backend_roots`, `backend_urls` e `app_url`. Devolve na hora `thread_id`, `run_id` e um status.
- `team_status`: para um `thread_id`, devolve `status` (a requisição e cada tarefa), `report`, `history` (os handoffs, incluindo cada rejeição de revisão e de QA e para quem voltou) e `running`. `status`, `report` e o `history` completo são preenchidos quando o último passo termina; enquanto a execução anda, o notificador de console imprime cada mudança de status em stderr.
- `team_queue`: os workers registrados (com o job atual) e os jobs na fila.
- `team_ask_worker`: faz uma pergunta a um worker, fora do fluxo de entrega. Um worker que responde assim não tem escopo de demanda, então as ferramentas de backend do PO recusam.
- `team_present`: chamada, veja [Chamada /present](#chamada-present). Cada worker esperado responde `Presente!` com id, papel e modelo.
- `team_memory`: recupera o que o time registrou para uma requisição (argumentos `scope`, o `thread_id` da requisição, e `query`).
- `graph_run`, `graph_state`, `graph_resume`: ferramentas genéricas para qualquer grafo em um Agent Server (`url` tem como padrão o manager).
- As ferramentas dos servidores citados em `BRIDGE_CIAM_CMD` e `BRIDGE_KNOWLEDGE_CMD`, se definidas.

### Registrar

Veja [Usar no VS Code](#usar-no-vs-code) para o registro no VS Code, no Claude Code e em outros clientes.

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

## Usar no VS Code

### Pré-requisitos

- O toolchain do Go 1.27 no `PATH` (`go version`; o `go.mod` diz `go 1.27`).
- O repositório clonado e aberto como pasta de workspace do VS Code. Assim `go run ./examples/langgraph/full_team_front serve` funciona com o workspace como diretório de trabalho, e `WORKSPACE_ROOT` tem ele como padrão. A alternativa é o binário compilado (`go build -o bin/team-mcp ./examples/langgraph/full_team_front`) iniciado por caminho absoluto, com `WORKSPACE_ROOT` definida.
- Opcional: `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY`. Sem chave o time roda offline com modelos roteirizados. No chat isso significa: todo pedido recebe o mesmo plano roteirizado de cupons (cinco tarefas, `T1` a `T5`), não importa o que você peça, e os revisores e o QA roteirizados devolvem `T2`, `T3` e `T4` uma vez. Isso basta para conferir a ligação e ver o fluxo; não é uma entrega real.

### Claude Code no VS Code

O Claude Code lê servidores MCP com escopo de projeto de `.mcp.json` na raiz do projeto (veja [independent_engineering_team](../independent_engineering_team/README.pt-BR.md#claude-code), cujos trechos este segue). Crie `.mcp.json` na raiz do workspace:

```json
{
  "mcpServers": {
    "front-team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": {
        "WORKSPACE_ROOT": "/abs/path/to/this/repo",
        "PLAYWRIGHT_CMD": "",
        "PLAYWRIGHT_ENV_ALLOW": ""
      }
    }
  }
}
```

Os valores são literais. Este README não depende da expansão `${VAR}` no `.mcp.json`, porque nem o README irmão nem o código a mostram. Não grave uma chave em um arquivo que você versiona: acrescente `ANTHROPIC_API_KEY` com `claude mcp add` (abaixo, escopo local) ou exporte-a no shell que inicia o VS Code e confira em `/mcp` que os modelos não estão offline. `PLAYWRIGHT_CMD` e `PLAYWRIGHT_ENV_ALLOW` podem ser omitidas para rodar sem a ferramenta de navegador. A forma com `go run` pressupõe que o servidor inicia com a raiz do repositório como diretório de trabalho; se não iniciar (não testado), use o binário com um `command` absoluto.

O mesmo servidor pela linha de comando (`--scope local` é o padrão; `project` grava `.mcp.json`; `user` vale para todos os projetos; `--` separa as opções do comando):

```bash
claude mcp add --transport stdio --scope project \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo \
  front-team -- go run ./examples/langgraph/full_team_front serve

claude mcp add --transport stdio --scope user \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo --env ANTHROPIC_API_KEY=... \
  front-team -- /abs/path/to/this/repo/bin/team-mcp serve
```

Servidores com escopo de projeto pedem aprovação na primeira vez. Confira o servidor com `claude mcp list`, ou com `/mcp` no chat: `front-team` deve aparecer como conectado e listar estas ferramentas: `team_submit`, `team_status`, `team_queue`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` e `graph_resume` (mais as ferramentas de `BRIDGE_CIAM_CMD` e `BRIDGE_KNOWLEDGE_CMD`, se você as definir); o servidor também lista o prompt `present`. Não foi testado aqui se o painel de chat da extensão do Claude Code no VS Code usa os mesmos arquivos de configuração e mostra a mesma tela `/mcp` que a CLI.

Se não conectar:

1. Execute o comando em um terminal, a partir da raiz do repositório: `go run ./examples/langgraph/full_team_front serve`. Ele não deve imprimir nada na saída padrão até um cliente falar (a saída padrão é só protocolo). As linhas de partida (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) aparecem em stderr. Pressione Ctrl-D para parar.
2. Leia essa saída de stderr. Se um worker não consegue iniciar (tipicamente um valor ruim em `AGENT_MODEL_<PAPEL>` ou a falta de chave do provedor escolhido), o processo termina com `team did not come up` após 10 segundos; o erro do próprio worker não é impresso, então confira primeiro essas variáveis.
3. Execute `go version` e confira que é 1.27 ou mais novo, e que `go` está no `PATH` do processo que inicia o servidor (um VS Code aberto por um lançador pode ter um `PATH` menor que o do seu shell).
4. Confira os caminhos: o diretório de trabalho do `go run`, o caminho absoluto do binário, `WORKSPACE_ROOT`. O primeiro `go run` compila o exemplo inteiro, então a primeira conexão pode ser lenta; o time desiste se os workers não subirem em 10 segundos, então prefira o binário compilado.

### Copilot Chat no VS Code (modo Agent)

Crie `.vscode/mcp.json` no workspace (o formato do VS Code usa `servers`, cada um com `"type": "stdio"`, como no [trecho do Copilot](../independent_engineering_team/README.pt-BR.md#github-copilot-vs-code) do README irmão). Forma com `go run`:

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

Outros clientes MCP (Cursor, Kilo Code, Opencode) aceitam um comando stdio no mesmo formato (`command`, `args`, `env`); os trechos em [independent_engineering_team](../independent_engineering_team/README.pt-BR.md#4-registre-em-cada-aplicação) mostram o arquivo de cada cliente, com `command` e `args` trocados pelos acima. Eles não foram testados com este servidor.

### Usar a partir do chat

Passos com prompts que você pode colar no chat do Claude Code (ou no modo Agent do Copilot). O chat decide quando chamar as ferramentas; citar a ferramenta faz com que ele a chame.

1. **Enviar uma demanda.** Prompt:

   > Use a ferramenta `team_submit` com request "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.", backend_roots ["/abs/path/to/this/repo/backend"], backend_urls ["http://localhost:8080"], app_url "http://localhost:3000/checkout". Depois me diga o thread_id.

   Os campos: `request` é o que o time deve entregar. `backend_roots` são as árvores de código que o PO pode ler; informe caminhos absolutos, cada um dentro de `WORKSPACE_ROOT` (um relativo é tomado em relação a ela, e um fora dela é recusado). `backend_urls` são as APIs que o PO pode chamar com `GET`; só esses hosts e portas são alcançáveis. `app_url` é o front-end em execução que o QA pode abrir com `browser_test` (exige `PLAYWRIGHT_CMD`). Os três são opcionais; sem raízes e URLs o PO não lê nada e a spec diz isso. A chamada retorna na hora:

   ```text
   thread_id=35a034fed3493843fc60f393894dd7ed run_id=646bef17962e894921b0a5e4fe5c0d92 status=pending
   ```

2. **Consultar o status.** Prompt: "Chame `team_status` com esse thread_id a cada poucos segundos até haver um `report`, e então me mostre o resultado." Os campos: `status` mapeia a requisição (chave `""`) e cada tarefa (`T1`...) para sua etapa (`received` ... `completed`); `report` é o resumo do manager; `history` é a lista de handoffs; `running` lista os nós que ainda vão rodar (`null` quando a execução terminou). `status`, `report` e o `history` completo são preenchidos quando o último passo termina, então consulte até `report` estar presente.

3. **Ler o histórico.** As devoluções da revisão conjunta e do QA aparecem em linhas como `T2: review returned by leader -> mid-1 | tech-leader: ...` e `T4: qa returned by qa-1 -> senior-1`. A linha seguinte daquela tarefa, `T2: mid-1 -> review`, mostra o mesmo desenvolvedor construindo de novo.

4. **Ver o time.** Prompt: "Chame `team_queue`." Ele devolve os workers registrados (`id`, `role`, `url`, `seen` e `job` enquanto um está ocupado; são 12) e os jobs em `queued`.

5. **Perguntar a um agente.** Prompt: "Chame `team_ask_worker` com worker `leader` e pergunta: quais desenvolvedores estão ocupados?" O id do worker vem de `team_queue`. A pergunta roda fora do fluxo de entrega, então não tem escopo de demanda: pedir ao `po` que leia o backend assim é recusado.

6. **Recuperar o que o time registrou.** Prompt: "Chame `team_memory` com scope <thread_id> e query `coupon`." O scope é o `thread_id` da requisição.

Para ler a spec do PO, chame `graph_state` com o `thread_id` e olhe `values.spec`.

Um exemplo completo, offline (sem chave, sem backend citado). Prompt do usuário: "Use a ferramenta team_submit com request 'Add a coupon field to checkout'." Chamada da ferramenta:

```json
{ "name": "team_submit", "arguments": { "request": "Add a coupon field to checkout" } }
```

Resultado: `thread_id=35a034fe... run_id=646bef17... status=pending`. Prompt do usuário: "Confira com team_status." Chamada da ferramenta `team_status` com `{ "thread_id": "35a034fe..." }`; o resultado offline, reduzido (o real lista todas as tarefas):

```json
{
  "history": [
    "manager -> product-owner",
    "product-owner (po) done",
    "tech-leader (leader) -> 5 tasks",
    "T2: mid-1 -> review",
    "T2: review returned by leader -> mid-1 | tech-leader: 1. The hook recomputes the discount in the component: show the discount_percent and total the API returned, ...",
    "T2: mid-1 -> review",
    "T2: review approved by specialist-1 and leader",
    "T2: qa approved by qa-1",
    "T2: approved",
    "T4: senior-1 -> review",
    "T4: review approved by specialist-1 and leader",
    "T4: qa returned by qa-1 -> senior-1",
    "T4: senior-1 -> review",
    "...",
    "manager -> requester"
  ],
  "report": "REPORT:\n- Shipped: T1, T5, T2, T3, T4\n- Blocked: none\n- Next: roll out behind a flag for 5% of checkout traffic.",
  "running": null,
  "status": { "": "completed", "T1": "completed", "T2": "completed", "T3": "completed", "T4": "completed", "T5": "completed" }
}
```

Solução de problemas:

- Ferramentas não listadas: o servidor não conectou; veja "Se não conectar" acima.
- `team_submit` retorna mas `team_status` nunca chega a um `report`: com chave real, um worker falhou ou uma chamada de modelo está falhando. Leia o stderr do servidor (execute o comando em um terminal para vê-lo) e confira a chave, os nomes de modelo (`AGENT_MODEL_<PAPEL>`) e a rede.
- A spec do PO tem `UNVERIFIED: no backend evidence` sob `OPEN ITEMS:`: a demanda citou um backend mas a spec não tem bloco `EVIDENCE:` ou o PO não fez nenhuma chamada ao backend que obtivesse resposta. As raízes ou URLs podem ter sido recusadas (fora de `WORKSPACE_ROOT`, um host não citado, um endereço bloqueado). Corrija os argumentos de `team_submit`. O mesmo marcador aparece se as ferramentas do PO não conseguiram reportar as chamadas ao hub (a falha é registrada no log como `backend: counting a call ...`, mas a chamada da ferramenta continua com sucesso) ou se o manager não conseguiu ler a contagem: a regra então falha para o lado seguro.
- `browser_test` diz `NOT RUN`: `PLAYWRIGHT_CMD` está vazia no ambiente do servidor, ou a demanda não tem `app_url` válida. O motivo vem depois de `NOT RUN:`.

### Chamada /present

Um easter egg para conferir que o chat chegou ao time: uma chamada de escola. A ferramenta `team_present` pede a cada worker esperado (os 12 deste exemplo) que responda uma linha começando com `Presente!` e dizendo seu id, papel e modelo, em paralelo e com no máximo 30 segundos cada. O prompt MCP `present` ("Roll call: every persona answers Presente") só manda o chat chamar essa ferramenta e imprimir o resultado, porque um prompt sozinho não faz ninguém responder. Se o chat não achar a ferramenta, o texto do prompt o faz dizer que a integração não está funcionando.

Como chamar: peça em palavras simples, "chame a ferramenta `team_present` e imprima a chamada", ou use o slash command. Grafias do slash (NÃO VERIFICADAS: não confirmadas nos clientes, tiradas de como eles nomeiam prompts MCP): no Claude Code `/mcp__<servidor>__present`, ou seja `/mcp__front-team__present` quando o servidor é registrado como `front-team` como acima; no Copilot Chat `/mcp.<servidor>.present`, aqui `/mcp.front-team.present`.

O que a chamada mostra: uma linha por worker com id, papel e modelo, depois `N of 12 present`, depois `Absent:` com os ids que não responderam. A coluna do modelo vem do próprio worker: ele informa o modelo com que foi construído ao se registrar no hub, então a chamada não depende de o modelo saber o próprio nome. Offline toda linha diz `offline`. Com `ANTHROPIC_API_KEY` definida, cada papel roda o seu modelo (`po` em `anthropic:claude-opus-5-5`; `leader`, `specialist` e `qa` em `anthropic:claude-sonnet-5-5`; `junior`, `mid` e `senior` em `anthropic:claude-haiku-5-5`), a menos que `AGENT_MODEL` ou `AGENT_MODEL_<ROLE>` o troque, então a chamada mostra se cada papel roda no modelo configurado para ele.

A chamada é conferida contra o time esperado, não só contra os workers que o hub conhece. Um worker que caiu e cujo lease de 15 segundos expirou saiu do hub, e mesmo assim ganha uma linha: `absent: not registered (lease expired or never started)`, e a contagem cai. Um worker registrado que falha, estoura o tempo ou não responde nada também fica ausente. Um worker registrado que não é um dos 12 aparece com `extra` e não conta no total. Presente significa uma resposta não vazia: um modelo real que ignore a instrução `Presente!` ainda conta como presente. Uma chamada boa mostra que a integração do chat funciona de ponta a ponta: chamada da ferramenta, hub, agente de cada worker, modelo.

Saída real do time offline por stdio (na ordem do elenco; as respostas são as do roteiro, um modelo real escreve as suas):

```
ROLL CALL
- po (po) [model offline] present: Presente! po (po), com a spec debaixo do braço
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- mid-2 (mid) [model offline] present: Presente! mid-2 (mid), no ritmo
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- senior-2 (senior) [model offline] present: Presente! senior-2 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
12 of 12 present
```

Com um worker fora por mais tempo que o lease (a mesma execução de teste, offline), a chamada termina assim:

```
- qa-2 (qa) [model unknown] absent: not registered (lease expired or never started)
11 of 12 present
Absent: qa-2
```

### Notas de segurança

- O servidor é um processo stdio local iniciado pelo seu editor, com as permissões do seu usuário e o ambiente que você deu a ele. O chat é a raiz de confiança: o que o chat passa em `team_submit` (`backend_roots`, `backend_urls`, `app_url`) vira o escopo da requisição.
- As leituras de arquivo do PO são limitadas por `WORKSPACE_ROOT` (o diretório de trabalho do servidor, se não definida) e pelas raízes citadas; symlinks e `..` não podem sair delas. Mantenha `WORKSPACE_ROOT` estreita.
- O argumento `url` de `graph_run`, `graph_state` e `graph_resume` permite que o chat faça a bridge chamar qualquer URL de Agent Server. A chave do manager só é enviada quando `url` está vazio ou é igual à URL do próprio manager; qualquer outra URL não recebe chave. `team_ask_worker` não tem `url`: alcança só os workers registrados no hub, em loopback.
- No modo serve nenhum papel tem a ferramenta `shell`: os desenvolvedores devolvem código como texto. A execução de demonstração offline mantém um shell endurecido (veja Shell em Como aplicar). O PO nunca tem shell.
- `browser_test` executa o comando que você definiu em `PLAYWRIGHT_CMD`, sem shell, com um ambiente mínimo: suas chaves de provedor não chegam a ele, a menos que listadas em `PLAYWRIGHT_ENV_ALLOW`. A spec que ele executa é escrita por um modelo.
- No modo serve o manager exige uma chave aleatória por execução que só a bridge do próprio processo possui; uma `MANAGER_API_KEY` definida pelo operador é ignorada. O hub e os servidores dos workers escutam em `127.0.0.1` e não têm autenticação, então qualquer processo local pode alcançá-los.
- O contador de chamadas da regra de evidência fica no hub (`POST /calls/{id}`, `GET /calls/{id}`), que escuta só em loopback e não tem autenticação: qualquer cliente local pode somar chamadas a uma requisição, enquanto o modelo não o alcança. Uma chamada tardia de uma execução anterior na mesma thread ainda pode contar depois que uma nova execução zera o contador.

## Como testar

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
go vet ./examples/langgraph/full_team_front/...
```

Um teste: `go test -race -count=1 -run TestJointReviewNeedsBothApprovals ./examples/langgraph/full_team_front/internal/team`. Os testes, por pacote:

- `main` (`main_test.go`, `serve_test.go`): `TestAllowedProgramsWithLiveModelButNoKey`, `TestAllowedProgramsWithProviderKey`, `TestScriptedPonytailFollowsRule`, `TestOfflineRunReportsTOON`, `TestPOGetsNoShell`, `TestServeStackHasNoShell`, `TestDemoShellRejectsExpansionAndRedirects`, `TestShellEnvHasNoSecrets`, `TestDemoShellConfinesPaths`, `TestDemoShellRejectsFlagAndExpansionBypasses`, `TestDemoShellRejectsGlobAndQuoteBypasses`, `TestDemoShellRejectsStructuralGlobBypasses`, `TestDemoShellRejectsBareBracketGlobThroughSymlink`, `TestShellRefusesDotGlobs`, `TestOpenTasksOnlyOffline`, `TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`, `TestServeStopsOnContextCancel`, `TestServeCancelDuringStartupIsClean`, `TestServeRestoresGlobals`, `TestServePresentOverStdio`. Os testes de serve também verificam que só o protocolo chega à saída padrão.
- `internal/team`: `TestBrainScenarioIsFrontEnd`, `TestGraphToolsSendKeyOnlyToManager`, `TestStartLocalWithHonoursCancel`, `TestManagerAPIKeyProtectsManager`, `TestOfflineRunPOUsesBackendTools`, `TestPresentToolRollCall`, `TestPresentAbsentWhenWorkerDown`, `TestPresentAbsentAfterLeaseExpired`, `TestPresentExtraWorkerNotCounted`, `TestPresentPromptListed`, `TestBrainRollCall`, `TestRegisterCarriesModel`, `TestQAPromptCarriesSpec`, `TestQAOutputTracesCriteria`, `TestDossierDropsUnsafeAppURL`, `TestSkillIncludesReferences`, `TestSkillUnknownRole`, `TestNoArchitectInSkills`, `TestSkillSectionsHaveNoFrontmatter`, `TestManagerSkillHasNoFences`, `TestReservationIsAtomic`, `TestPreferenceOrder`, `TestLeaseExpiryRequeues`, `TestJobFailsAfterMaxAttempts`, `TestKnowledgeAndMemoryOverMCP`, `TestNormalizeModel`, `TestTeamDeliversOffline`, `TestBridgeQueueAndAskUnknown`, `TestBridgeToolSetAndProxy`, `TestStartLocalWithMiddleware`, `TestCastHasTwelveWorkers`, `TestDefaultModels`, `TestPinnedJobWaitsForItsWorker`, `TestPinnedJobFallsBackWhenLeaseExpired`, `TestJointReviewNeedsBothApprovals`, `TestRejectedWorkReturnsToSameDeveloper`, `TestPlanParsesPreferField`, `TestThreadScopeFailsClosedOnAmbiguity`, `TestPlanValidatesPrefer`, `TestPlanNormalisesComplexity`, `TestTaskBlockedAfterMaxRounds`, `TestScopeRoundTripOverHTTP`, `TestSubmitStoresScope`, `TestToolSeesJobScope`, `TestScopeNeverResolvedByPrefix`, `TestScopeIdCollision`, `TestPOSpecWithoutEvidenceIsMarkedUnverified`, `TestVerifiedSpec`, `TestHubCountsCallsAndSetScopeResets`, `TestCallsRoundTripOverHTTP`, `TestEvidenceRuleWorksAcrossServices`.
- `internal/backend`: `TestReadRefusesOutsideRoots`, `TestReadWithinRoot`, `TestGrepStaysInsideRoot`, `TestGrepLimits`, `TestRootsResolveAgainstWorkspace`, `TestGetRefusesNonGetAndUnnamedHost`, `TestGetRefusesRedirectToOtherHost`, `TestGetRefusesLoopbackUnlessNamed`, `TestGetNamedLocalhost`, `TestGetBodyCapped`, `TestNoScopeMeansNoAccess`, `TestReportsGuardedCallsPerRequest`, `TestReportFailureDoesNotFailTheCall`.
- `internal/qa`: `TestBrowserTestNotRunWithoutCommand`, `TestBrowserTestNotRunWithoutAppURL`, `TestBrowserTestRejectsNonHTTPAppURL`, `TestBrowserTestRunsRunnerWithScopeURL`, `TestCommandRunnerUsesNoShell`, `TestCommandRunnerCapsOutput`, `TestCommandRunnerMinimalEnv`, `TestCommandRunnerTimeoutKillsProcessGroup`, `TestToolNotGivenToOtherRoles`.
- `internal/tasktrack`: `TestTaskFlowAllowsRework`, `TestIllegalMoveIsRejectedAndNotRecorded`, `TestWebhookSinksAndFilter`, `TestNotifierFailureDoesNotBlock`, `TestFromEnvAndEmailMessage`.
- `skills`: `TestEveryLockEntryIsEmbedded`, `TestVendoredSkillWithinBudget`, `TestEveryLockEntryHasLicense`, `TestEveryRoleDirHasOwnCopies`, `TestNoticesCoverLockedLicensesAndSources`, `TestPinnedFromIsPlausibleCommit`.

Os testes de serve que acionam o time offline (`TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`) são pulados quando algum papel tem especificação de modelo (uma chave ou `AGENT_MODEL*` está definida).

## Como testar em modo de produção

1. Registre o servidor (veja acima) com `ANTHROPIC_API_KEY` (ou `OPENAI_API_KEY`). No log do servidor em stderr, cada worker imprime `up on ... (model ...)`; confira se os modelos são os que você definiu (`AGENT_MODEL_<PAPEL>` sobrescreve os padrões).
2. Envie uma demanda pelo chat MCP com `team_submit`, com `backend_roots` e/ou `backend_urls` apontando para um backend real (e `app_url` para a execução no navegador). Consulte com `team_status` até `report` estar presente.
3. No `history` de `team_status`, procure uma rejeição da revisão conjunta (`review returned by ... -> <desenvolvedor>`) e uma rejeição do QA (`qa returned by ... -> <desenvolvedor>`). A linha seguinte daquela tarefa deve mostrar o mesmo desenvolvedor construindo de novo (`<tarefa>: <desenvolvedor> -> review`). Modelos nem sempre rejeitam; se nenhum rejeitou, envie uma demanda com uma lacuna proposital.
4. Confira a spec do PO: chame `graph_state` com o `thread_id` e leia `values.spec`. Ela deve conter um bloco `EVIDENCE:` em que cada endpoint corresponda ao backend para o qual você apontou, e os itens em aberto não devem conter `UNVERIFIED: no backend evidence`. Esse marcador significa que o PO não leu o backend.
5. Tente uma demanda que cita um host que o PO não deveria alcançar, e uma URL que redireciona para outro host. As duas devem ser recusadas na saída da ferramenta.
6. Sem `PLAYWRIGHT_CMD`, ou sem `app_url`, o resultado do `browser_test` do QA deve dizer `NOT RUN` com o motivo. Com as duas definidas (e `PLAYWRIGHT_BROWSERS_PATH`, e o Chrome instalado onde o servidor roda), deve mostrar a saída do Playwright.
7. Mantenha `WORKSPACE_ROOT` e as raízes de backend tão estreitas quanto o trabalho permitir: as ferramentas de backend leem arquivos no host que roda o servidor. No modo serve nenhum papel tem shell. Se você rodar a demonstração com um modelo real, mantenha enxuta a lista de comandos permitidos do shell (`ls`, `grep` e `echo`); o shell não tem sandbox.

## Veja também

- [full_team](../full_team/README.pt-BR.md): o time copiado, com a camada de compressão documentada por completo
- [independent_engineering_team](../independent_engineering_team/README.pt-BR.md): o mesmo time dividido em serviços, com Docker compose, e os trechos de registro de cada cliente MCP
- [compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md), [compress_toon](../compress_toon/README.pt-BR.md), [compress_ponytail](../compress_ponytail/README.pt-BR.md)
