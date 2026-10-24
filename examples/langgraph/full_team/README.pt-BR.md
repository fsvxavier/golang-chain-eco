# full_team

[English](README.md) | **Português** | [Español](README.es-ES.md)

O fluxo de engenharia de software (hub, gerente, workers via Agent Protocol e MCP, memória do time, leases) com a camada `compress` em cada worker: as chamadas de modelo de cada agente são comprimidas, a ferramenta de shell direciona comandos simples pelo `rtk`, e duas outras reescritas rodam no mesmo middleware: **Ponytail** (uma nota pedindo o menor código possível) e **TOON** (resultados de ferramentas em JSON escritos numa forma mais curta). Offline roda com um decisor roteirizado, então o efeito de cada alavanca aparece sem modelo.

Esta pasta é autocontida. Ela não importa `examples/langgraph/independent_engineering_team`; o código foi copiado de lá.

## O que faz

- **Copiado de `independent_engineering_team`**: `internal/team` (hub, gerente, workers, conhecimento, memória do time, testes), `skills/` (personas dos agentes) e `kb/` (padrões e checklists). A cópia acrescenta só os ganchos abaixo, então o original fica intacto.
- **Ganchos acrescentados ao time**: `WorkerConfig.Middleware` e `WorkerConfig.Tools` (vazio significa sem mudança), `LocalOptions` e `StartLocalWith`, que entrega middleware e ferramentas a cada worker. `StartLocal` é `StartLocalWith` sem nada extra. `TestStartLocalWithMiddleware` confere que o middleware do worker roda.
- **Chamadas de modelo**: cada agente de worker recebe `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Turnos antigos (Human e AI) são comprimidos com caveman a cada chamada; chamadas e resultados de ferramentas mantêm ids e ordem. O histórico guardado não é reescrito.
- **Ponytail**: quando o decisor responde `ponytail`, a última mensagem Human de cada cópia por chamada recebe a diretiva Ponytail (subir a escada: precisa existir, reutilizar o que o código já tem, biblioteca padrão, recurso nativo, dependência instalada, uma linha; preguiçoso com a solução, nunca com a leitura). Ela nunca pede ao modelo para pular validação, tratamento de erro, segurança, acessibilidade ou os testes que protegem lógica arriscada. A nota não é guardada e não entra no system prompt.
- **TOON**: quando o decisor responde `toon_tool_results`, cada resultado de ferramenta cujo texto é um objeto ou array JSON é reescrito em TOON, mas só quando o texto TOON é estritamente menor. Offline, cada worker chama a ferramenta `open_tasks` uma vez por turno (`team.OfflineWithTool`): ela devolve o backlog como um array JSON de objetos uniformes, que o TOON escreve como tabela. O resultado chega ao modelo e é reescrito. Saídas de outras ferramentas não são tocadas.
- **Shell**: cada worker recebe a ferramenta `shell` (`{command}`) sobre `backends.LocalShell`, com `AllowedCommands` (`go git ls grep echo` offline; `ls grep echo` quando há chave de provedor ou algum papel usa modelo real, veja `allowedPrograms`) e `Rewrite: rtk.Rewrite`. `RTK.Env` é o `Env` do shell, então o `rtk` é procurado no mesmo PATH que o shell usa. `rtk.Executor` registra o tamanho da saída para a decisão de roteamento. Os comandos veem um ambiente mínimo (`PATH` e um `HOME` dentro do diretório temporário, então nenhuma chave de provedor chega a eles). Um comando com `<`, `>`, `$`, crase, barra invertida ou chave é recusado. Argumentos de caminho e os valores de `--flag=valor` e de flags curtas coladas (`-f/etc/x`) que resolvam fora do workspace, links simbólicos incluídos, são recusados (`shell: paths outside the workspace are not allowed`). Flags que leem um arquivo de padrões ou seguem links simbólicos (`grep -f`, `--file`, `--exclude-from`, `--include-from`, `-R`) e `-C`, `--git-dir`, `--work-tree` em `go` e `git` são negadas. Um glob em um argumento de caminho é recusado quando seu prefixo literal (a parte antes do primeiro `*` ou `?`) está fora do workspace, quando contém `[` ou quando tem `..` depois de um curinga; padrões soltos como `[0-9]` ou `*.go` e globs relativos ao workspace como `src/*.tsx` funcionam. Um curinga que pode expandir para `.` ou `..` (`.*`, `.?`) é recusado, e um glob de colchetes solto é verificado por um padrão superconjunto (cada `[...]` lido como `?`), então uma correspondência que saia do workspace é recusada. Um nome que não existe em disco, como um padrão de grep `/api/coupon`, é permitido. É uma proteção para a demo, não um sandbox.
- **Decisor**: roteirizado offline (responde com confiança a tudo, então compressão e TOON se aplicam). `ponytail` é respondido pela regra (`compress.RuleDecider`), então a nota aparece só onde a conversa tem uma parcela real de blocos de código. Com `LAYA_URL` definido, `compress.HTTPDecider` consulta um endpoint do laya-serve.
- **Relatório**: ao fim, imprime quantas chamadas de modelo foram reescritas, os tokens estimados antes e depois (bytes/4), quantas chamadas levaram a nota Ponytail, quantos resultados de ferramenta foram reescritos em TOON com os bytes economizados, e se o `rtk` está no PATH. As contagens vêm de um wrapper do middleware (`meter`), não de mudanças na biblioteca. Uma execução offline mostrou 46 chamadas de modelo, 31 reescritas TOON por chamada economizando 11625 bytes, e 46114 tokens estimados antes e 42964 depois. Os números impressos variam um pouco entre execuções (tempo das goroutines). A execução offline pode não mostrar nenhuma nota Ponytail: a regra só acrescenta uma quando a conversa tem parcela real de código, e a execução roteirizada tem pouco. A nota Ponytail acrescenta cerca de 0,8 KB a uma chamada; ela só compensa quando a saída do modelo encolhe, o que a execução offline roteirizada não mede.

`native/` é o mesmo programa com o decisor respondido por um modelo laya em processo. É um pacote do módulo raiz: `native.go` tem a build tag `laya_native` e importa `laya-go`; `stub.go` tem `!laya_native` e é usado por padrão. Seu fallback roteirizado não tem a ferramenta `open_tasks`, então o relatório dele não mostra reescritas TOON offline. Esse fallback responde `ponytail` com não, e o medidor dele não conta notas Ponytail.

## Como aplicar

- Middleware: `Keep` é quantos turnos recentes ficam como estão (padrão 4; esta demo usa 1 porque as threads são curtas).
- Shell: `AllowedCommands` em `shellTool`. Nunca liste shells, interpretadores, `env`, `xargs` ou `find`. `go` e `git` podem executar código, então o host precisa de sandbox para qualquer entrada não confiável. Eles só são oferecidos na execução offline com script: um modelo real (chave de provedor, `AGENT_MODEL` ou `AGENT_MODEL_<ROLE>`) recebe só `ls`, `grep` e `echo`.
- Decisor: `decider()` em `main.go`. Envolva qualquer decisor com `compress.NewGuarded`, para que um decisor lento ou errado caia nas regras.
- Desligar o Ponytail: offline, a regra o responde; para forçar o desligamento, responda `P: 0.05` para `compress.QPonytail` em `scripted()`. Desligue o TOON da mesma forma, para `compress.QTOON`. Com laya, valem as respostas do decisor; envolva-o para que esses dois ids voltem sempre falsos, se quiser desligá-los ali. Desligar um ou os dois não muda as ferramentas nem o resto do fluxo.
- Ligar o TOON nas suas ferramentas: qualquer ferramenta cujo resultado seja um objeto ou array JSON se beneficia, desde que seja uma lista uniforme de objetos com campos primitivos; `open_tasks` em `main.go` é o exemplo.

## Como rodar

Offline, um processo, HTTP real entre os serviços:

```bash
go run ./examples/langgraph/full_team
```

Contra um endpoint do laya-serve:

```bash
LAYA_URL=http://localhost:8099/v1/systemone LAYA_API_KEY=... go run ./examples/langgraph/full_team
```

Nativo (modelo laya em processo):

```bash
export LAYA_ONNXRUNTIME_LIBRARY=/caminho/para/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/caminho/para/libtokenizers
export LAYA_BUNDLE_DIR=/caminho/para/bundle
CGO_ENABLED=1 go run -tags laya_native ./examples/langgraph/full_team/native
```

Sem `-tags laya_native`, ou sem `LAYA_BUNDLE_DIR`, o programa nativo usa o decisor roteirizado e diz o motivo. A build nativa precisa de linux/amd64 e modelos FP32, como em `decider_laya_native`.

## Integração com o chat e /present

`serve` roda o mesmo time em um processo e o expõe como servidor MCP em stdio, para que um chat (Claude Code, Copilot Chat em modo Agent) o controle:

```bash
go run ./examples/langgraph/full_team serve
go build -o bin/team-mcp ./examples/langgraph/full_team && ./bin/team-mcp serve
```

O stdout leva só o protocolo MCP. As linhas de inicialização (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) vão para o stderr. Ele para no fim da entrada (Ctrl-D) ou com um sinal. O modo serve difere da demo em três pontos: nenhum worker recebe a ferramenta `shell`, o manager roda com uma API key aleatória gerada na partida (um `MANAGER_API_KEY` definido pelo operador é ignorado; só a bridge deste processo conhece a chave) e nenhuma demanda é enviada até o chat chamar `team_submit`. As ferramentas são `team_submit`, `team_status`, `team_queue`, `team_cancel`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` e `graph_resume`.

Mais dois fatos sobre o modo serve. O argumento `url` de `graph_run`, `graph_state` e `graph_resume` permite que o chat faça a bridge chamar qualquer URL de Agent Server; a chave do manager só é enviada quando `url` está vazio ou é igual à URL do próprio manager, e qualquer outra URL não recebe chave. O hub e os servidores dos workers escutam em `127.0.0.1` e não têm autenticação, então qualquer processo local pode alcançá-los.

Registre no cliente (mesmos formatos de [Use it from VS Code](../full_team_front/README.pt-BR.md#usar-no-vs-code) no `full_team_front`, que este segue; o primeiro `go run` compila o exemplo, e o time desiste após 10 segundos se os workers não subirem, então prefira o binário compilado se o chat estourar o tempo). `.mcp.json` do Claude Code, na raiz do projeto:

```json
{
  "mcpServers": {
    "team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

ou pela linha de comando:

```bash
claude mcp add --transport stdio --scope project team -- go run ./examples/langgraph/full_team serve
claude mcp add --transport stdio --scope user --env ANTHROPIC_API_KEY=... team -- /abs/path/to/this/repo/bin/team-mcp serve
```

`.vscode/mcp.json` para o Copilot Chat (habilite o servidor no seletor de ferramentas do modo Agent):

```json
{
  "servers": {
    "team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

Nem o painel da extensão Claude Code no VS Code nem o trecho do Copilot foram testados contra este servidor aqui; são os formatos que o README irmão usa.

### /present

Um easter egg para conferir que o chat chegou ao time: uma chamada de escola. Cada worker registrado é consultado, em paralelo (30 segundos no máximo cada), e deve responder uma linha que começa com `Presente!` e cita seu id, papel e modelo. A ferramenta `team_present` faz as consultas e devolve a chamada. O prompt MCP `present` ("Roll call: every persona answers Presente") só manda o chat chamar essa ferramenta e mostrar o resultado, porque um prompt sozinho não faz ninguém responder. Se o chat não achar a ferramenta, o texto do prompt o faz dizer que a integração não está funcionando.

Grafia do slash (NÃO VERIFICADA: não confirmada nos clientes, tirada de como eles nomeiam prompts MCP): no Claude Code `/mcp__team__present` quando o servidor está registrado como `team`; no Copilot Chat `/mcp.team.present`. Se o slash não aparecer, peça em palavras: "chame a ferramenta team_present e mostre a chamada".

A coluna de modelo vem do próprio worker: ele informa ao hub, ao se registrar, o modelo com que foi criado, então a chamada não depende de o modelo saber o próprio nome. Offline toda linha diz `offline`. Com `ANTHROPIC_API_KEY` definida, cada papel roda o seu modelo (`po` e `architect` em `anthropic:claude-opus-5-5`; `leader`, `senior` e `specialist` em `anthropic:claude-sonnet-5-5`; `junior`, `mid` e `qa` em `anthropic:claude-haiku-5-5`), a menos que `AGENT_MODEL` ou `AGENT_MODEL_<ROLE>` o substitua, então a chamada mostra cada papel no modelo configurado para ele. A chamada parte do elenco esperado (os 11 workers do cast) combinado com o que o hub lista, então um worker que caiu e perdeu o lease, ou que nunca subiu, ainda ganha uma linha (`absent: not registered (lease expired or never started)`) e o resumo diz `10 of 11 present` seguido de `Absent: <id>`. Um worker que falha, estoura o tempo ou não responde nada também aparece como ausente. Um worker registrado fora do elenco aparece com a marca `extra (not in the roster, not counted)` e não é contado; o resumo é sempre `N of M present`, com M o tamanho do elenco. Saída real do time offline (11 workers, ordenados por id; as respostas são as roteirizadas, um modelo real redige as suas):

```text
ROLL CALL
- architect (architect) [model offline] present: Presente! architect (architect), com o diagrama na mão
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- po (po) [model offline] present: Presente! po (po), com o backlog debaixo do braço
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
11 of 11 present
```

Com um worker fora, as últimas linhas são `10 of 11 present` e `Absent: qa-2`.

## O PO mantém os documentos do projeto

O product owner é o único papel que pode gravar, e só por `project_doc_write`: arquivos markdown (`.md`, até 256 KB), com `replace` ou `append`. O local é definido por cada demanda: o campo opcional `docs_roots` do `team_submit` lista os diretórios (relativos ao workspace, ou absolutos sob `WORKSPACE_ROOT`, senão o diretório de trabalho do servidor) onde o PO pode gravar, e um caminho relativo na ferramenta começa no primeiro. Uma demanda sem `docs_roots` não dá ao PO nenhum acesso de escrita. Todo o resto é recusado: outras extensões, caminhos fora dos diretórios citados, `..` e symlinks que levam para fora. Os diretórios chegam à ferramenta pelo hub, pela thread exata do job (`internal/docs`), e o manager os lista no prompt do PO. Um modelo decide o conteúdo: revise o `git diff` desses arquivos depois de uma execução. Código-fonte, e todos os outros papéis, continuam somente leitura para ele.

## Painel de acompanhamento e cancelamento

Uma página web somente leitura mostra cada pedido e tarefa com status, barra de progresso, tempo de execução e uso de tokens, e cada persona (worker) com estado, job atual, skill, o que está fazendo agora (a referência que consulta, a ferramenta que chama ou o modelo), jobs concluídos, tempo ocupado e tokens. Os tokens também são somados por tarefa, por pedido e no projeto; as chamadas do modelo de conhecimento não entram na conta. Atualiza a cada 2 segundos e mostra apenas status, tempos e contagem de tokens, nunca prompts ou saídas.

- O modo `serve` abre em `http://127.0.0.1:8088` (somente loopback); as outras execuções só o servem com `DASHBOARD_ADDR=host:porta`. A página não tem autenticação: para outra máquina use `DASHBOARD_ADDR=0.0.0.0:8088` atrás de um proxy com autenticação.
- Cada projeto tem sua própria URL, `http://127.0.0.1:8088/p/<projeto>/` (`/?project=<projeto>` redireciona para ela), e o cabeçalho tem um seletor de projeto. O primeiro time a subir é dono da porta e lista os demais; um time que a encontra ocupada serve numa porta de loopback livre e se registra no dono (somente loopback). O projeto é `WORKSPACE_ROOT`, senão o diretório de trabalho.
- `team_cancel` (ferramenta MCP, argumento `thread_id`) cancela um pedido: os jobs na fila são descartados, os agentes em execução param no próximo heartbeat e a entrega termina como `cancelled`. A chamada ao modelo em curso do manager ainda termina, e os tokens de um job cancelado não são contados. Enviar a mesma thread de novo começa uma execução limpa.
- O agente do próprio manager passa pelo mesmo middleware de compressão dos workers.

## Como testar

```bash
go test -race -count=1 ./examples/langgraph/full_team/...   # inclui TestStartLocalWithMiddleware e TestOfflineRunReportsPonytailAndTOON (execução offline: ao menos uma nota Ponytail e uma reescrita TOON com bytes economizados)
```

## Como testar em modo de produção

1. Defina `LAYA_URL` (ou `LAYA_BUNDLE_DIR`, no nativo) e confira se o relatório mostra chamadas reescritas e tokens caindo em threads longas. Threads curtas podem não ter economia, ou ter um pequeno aumento pela diretiva de resposta e pela nota Ponytail.
2. Compare a contagem de Ponytail com a parte de código: o decisor laya decide por chamada, e uma chamada com a nota só vale a pena se a resposta tiver menos código. Compare os tokens de saída com e sem `ponytail` na mesma requisição antes de mantê-lo ligado.
3. Observe a latência da primeira chamada: o timeout de 2s do guard cai nas regras, e o relatório não separa essas respostas das do laya. Compare com uma execução sem `LAYA_URL`.
4. Confirme `rtk on PATH: true` na máquina que roda os workers. Sem ele, os comandos de shell passam sem mudança.
5. Mantenha `AllowedCommands` restrito: o `LocalShell` roda no host, sem sandbox.

## Veja também

- [independent_engineering_team](../independent_engineering_team/README.pt-BR.md): a origem do time copiado, sem compressão
- [compress_history_summary](../compress_history_summary/README.pt-BR.md), [compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md), [decider_laya_http](../decider_laya_http/README.pt-BR.md), [decider_laya_native](../decider_laya_native/README.pt-BR.md)
