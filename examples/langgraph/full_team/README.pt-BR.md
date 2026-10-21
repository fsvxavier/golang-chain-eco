# full_team

[English](README.md) | **Português** | [Español](README.es-ES.md)

O fluxo de engenharia de software (hub, gerente, workers via Agent Protocol e MCP, memória do time, leases) com a camada `compress` em cada worker: as chamadas de modelo de cada agente são comprimidas, e a ferramenta de shell direciona comandos simples pelo `rtk`. Offline roda com um decisor roteirizado, então a economia aparece sem modelo.

Esta pasta é autocontida. Ela não importa `examples/langgraph/independent_engineering_team`; o código foi copiado de lá.

## O que faz

- **Copiado de `independent_engineering_team`**: `internal/team` (hub, gerente, workers, conhecimento, memória do time, testes), `skills/` (personas dos agentes) e `kb/` (padrões e checklists). A cópia acrescenta só os ganchos abaixo, então o original fica intacto.
- **Ganchos acrescentados ao time**: `WorkerConfig.Middleware` e `WorkerConfig.Tools` (vazio significa sem mudança), `LocalOptions` e `StartLocalWith`, que entrega middleware e ferramentas a cada worker. `StartLocal` é `StartLocalWith` sem nada extra. `TestStartLocalWithMiddleware` confere que o middleware do worker roda.
- **Chamadas de modelo**: cada agente de worker recebe `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Turnos antigos (Human e AI) são comprimidos com caveman a cada chamada; chamadas e resultados de ferramentas nunca são tocados. O histórico guardado não é reescrito.
- **Shell**: cada worker recebe a ferramenta `shell` (`{command}`) sobre `backends.LocalShell`, com `AllowedCommands` (`go git ls grep echo`) e `Rewrite: rtk.Rewrite`. `RTK.Env` é o `Env` do shell, então o `rtk` é procurado no mesmo PATH que o shell usa. `rtk.Executor` registra o tamanho da saída para a decisão de roteamento.
- **Decisor**: roteirizado offline (responde com confiança em tudo, então a compressão se aplica). Com `LAYA_URL` definido, `compress.HTTPDecider` consulta um endpoint do laya-serve.
- **Relatório**: ao fim, imprime quantas chamadas de modelo foram reescritas, os tokens estimados antes e depois (bytes/4) e se o `rtk` está no PATH.

`native/` é o mesmo programa com o decisor respondido por um modelo laya em processo. É um pacote do módulo raiz: `native.go` tem a build tag `laya_native` e importa `laya-go`; `stub.go` tem `!laya_native` e é usado por padrão.

## Como aplicar

- Middleware: `Keep` é quantos turnos recentes ficam como estão (padrão 4; esta demo usa 1 porque as threads são curtas).
- Shell: `AllowedCommands` em `shellTool`. Nunca liste shells, interpretadores, `env`, `xargs` ou `find`. `go` e `git` podem executar código, então o host precisa de sandbox para qualquer entrada não confiável.
- Decisor: `decider()` em `main.go`. Envolva qualquer decisor com `compress.NewGuarded`, para que um decisor lento ou errado caia nas regras.

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
go test -race -count=1 ./examples/langgraph/full_team/...   # inclui TestStartLocalWithMiddleware
```

## Como testar em modo de produção

1. Defina `LAYA_URL` (ou `LAYA_BUNDLE_DIR`, no nativo) e confira se o relatório mostra chamadas reescritas e tokens caindo em threads longas. Threads curtas podem não ter economia, ou ter um pequeno aumento pela diretiva de resposta.
2. Observe a latência da primeira chamada: o timeout de 2s do guard cai nas regras, e o relatório não separa essas respostas das do laya. Compare com uma execução sem `LAYA_URL`.
3. Confirme `rtk on PATH: true` na máquina que roda os workers. Sem ele, os comandos de shell passam sem mudança.
4. Mantenha `AllowedCommands` restrito: o `LocalShell` roda no host, sem sandbox.

## Veja também

- [independent_engineering_team](../independent_engineering_team/README.pt-BR.md): a origem do time copiado, sem compressão
- [compress_history_summary](../compress_history_summary/README.pt-BR.md), [compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md), [decider_laya_http](../decider_laya_http/README.pt-BR.md), [decider_laya_native](../decider_laya_native/README.pt-BR.md)
