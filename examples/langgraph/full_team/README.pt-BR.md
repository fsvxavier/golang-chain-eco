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
- **Shell**: cada worker recebe a ferramenta `shell` (`{command}`) sobre `backends.LocalShell`, com `AllowedCommands` (`go git ls grep echo` offline; `ls grep echo` quando `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` está definida, veja `allowedPrograms`) e `Rewrite: rtk.Rewrite`. `RTK.Env` é o `Env` do shell, então o `rtk` é procurado no mesmo PATH que o shell usa. `rtk.Executor` registra o tamanho da saída para a decisão de roteamento.
- **Decisor**: roteirizado offline (responde com confiança a tudo, então compressão e TOON se aplicam). `ponytail` é respondido pela regra (`compress.RuleDecider`), então a nota aparece só onde a conversa tem uma parcela real de blocos de código. Com `LAYA_URL` definido, `compress.HTTPDecider` consulta um endpoint do laya-serve.
- **Relatório**: ao fim, imprime quantas chamadas de modelo foram reescritas, os tokens estimados antes e depois (bytes/4), quantas chamadas levaram a nota Ponytail, quantos resultados de ferramenta foram reescritos em TOON com os bytes economizados, e se o `rtk` está no PATH. As contagens vêm de um wrapper do middleware (`meter`), não de mudanças na biblioteca. Uma execução offline mostrou 46 chamadas de modelo, 31 reescritas TOON por chamada economizando 11625 bytes, e 46114 tokens estimados antes e 42964 depois. Os números impressos variam um pouco entre execuções (tempo das goroutines). A execução offline pode não mostrar nenhuma nota Ponytail: a regra só acrescenta uma quando a conversa tem parcela real de código, e a execução roteirizada tem pouco. A nota Ponytail acrescenta cerca de 0,8 KB a uma chamada; ela só compensa quando a saída do modelo encolhe, o que a execução offline roteirizada não mede.

`native/` é o mesmo programa com o decisor respondido por um modelo laya em processo. É um pacote do módulo raiz: `native.go` tem a build tag `laya_native` e importa `laya-go`; `stub.go` tem `!laya_native` e é usado por padrão. Seu fallback roteirizado não tem a ferramenta `open_tasks`, então o relatório dele não mostra reescritas TOON offline. Esse fallback responde `ponytail` com não, e o medidor dele não conta notas Ponytail.

## Como aplicar

- Middleware: `Keep` é quantos turnos recentes ficam como estão (padrão 4; esta demo usa 1 porque as threads são curtas).
- Shell: `AllowedCommands` em `shellTool`. Nunca liste shells, interpretadores, `env`, `xargs` ou `find`. `go` e `git` podem executar código, então o host precisa de sandbox para qualquer entrada não confiável. Eles só são oferecidos na execução offline com script: um modelo real com chave de provedor recebe só `ls`, `grep` e `echo`.
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
