# decider_laya_native

[English](README.md) | **Português** | [Español](README.es-ES.md)

Responde as perguntas de histórico e de shell com um modelo Laya dentro do próprio processo, por meio de `laya-go` e `compress.Decider`. Sem suporte nativo, cai nas regras, então o exemplo sempre roda.

Este é um módulo Go próprio (`replace github.com/fsvxavier/golang-chain-eco => ../../..`). Seu `go.mod` exige `github.com/metalagman/laya-go`; o `go.mod` da raiz também o exige, para `full_team/native` (atrás da tag `laya_native`).

## O que faz

- `native.go` (build tag `laya_native`) implementa `compress.Decider` com `laya-go`: `NewRuntime`, `OpenModelDir` (com `TruncateOverflow`, para que históricos longos sejam encurtados em vez de falhar com `ErrInputTooLong`) e uma chamada `Predict` por lote. Define `ORT_DISABLE_TELEMETRY=1`.
- Perguntas `noul` viram `NewNoulQuestion`; perguntas `choice` viram `NewChoiceQuestion`, com um critério por opção.
- `stub.go` (build tag `!laya_native`) devolve um erro, então o `main` mostra o motivo e usa o `compress.RuleDecider`.
- `main.go` mostra cada resposta com a origem: `laya` ou `rules`.

## Requisitos

- Linux em amd64 (`linux/amd64`), somente modelos FP32.
- `laya-go` v0.2.6, versão pré-v1: a API pode mudar antes da v1.
- Para o build nativo: cgo, a biblioteca compartilhada do ONNX Runtime e a biblioteca de tokenizers, ambas fornecidas pelo chamador e fixadas em versão, e um bundle Laya completo e local.

Variáveis de ambiente para o build nativo:

| Variável | Significado |
|---|---|
| `LAYA_ONNXRUNTIME_LIBRARY` | caminho da biblioteca compartilhada do ONNX Runtime |
| `LAYA_TOKENIZERS_LIBRARY` | caminho da biblioteca de tokenizers |
| `LAYA_BUNDLE_DIR` | diretório de um bundle Laya completo (lido pelo `main`; sem ele, usa regras) |

## Como aplicar

```go
rt, _ := laya.NewRuntime(ctx, laya.RuntimeOptions{})
m, _ := rt.OpenModelDir(ctx, dir, laya.ModelOptions{QueueCapacity: 8, Truncation: laya.TruncateOverflow})
g := compress.NewGuarded(nativeDecider{model: m}) // cai nas regras em caso de erro
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Como executar

Caminho stub (sem bibliotecas nativas, sem cgo):

```bash
cd examples/langgraph/decider_laya_native
go run .
```

Caminho nativo:

```bash
cd examples/langgraph/decider_laya_native
export LAYA_ONNXRUNTIME_LIBRARY=/caminho/para/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/caminho/para/libtokenizers
export LAYA_BUNDLE_DIR=/caminho/para/bundle
CGO_ENABLED=1 go run -tags laya_native .
```

Sem `LAYA_BUNDLE_DIR`, o build nativo também usa as regras e informa isso.

## Teste em modo de produção

1. Compile com `-tags laya_native` no CI em linux/amd64, com as bibliotecas fixadas, e confira que a saída mostra `laya` nas perguntas.
2. Meça a latência do primeiro `Predict` após a inicialização; mantenha o `Timeout` (2s por padrão em `compress.NewGuarded`) acima dela, senão a execução cai nas regras.
3. Feche o modelo antes do runtime (o exemplo faz isso no closer). Mantenha o diretório do bundle imutável enquanto o modelo estiver aberto.
4. Fixe o `laya-go` em uma versão exata e revise o changelog antes de cada atualização, já que a API é pré-v1.

## Veja também

- [decider_laya_http](../decider_laya_http/README.pt-BR.md): as mesmas perguntas contra um endpoint do laya-serve
- [decider_fallback](../decider_fallback/README.pt-BR.md): o decider por regras e o guard isoladamente
