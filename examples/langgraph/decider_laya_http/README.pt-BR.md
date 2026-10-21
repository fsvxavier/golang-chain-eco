# decider_laya_http

[English](README.md) | **Português** | [Español](README.es-ES.md)

Pergunta a um endpoint do laya-serve (`POST /v1/systemone`, o mesmo protocolo do TypeSafe Jev) as respostas de histórico e de shell, por meio de `compress.HTTPDecider` envolvido em `compress.NewGuarded`.

## O que faz

- Monta uma conversa de 3 mensagens e faz as três perguntas de histórico (`compress_history`, `level`, `compress_reply`) e a pergunta de shell (`route_rtk`).
- Sem `LAYA_URL`, sobe dentro do próprio processo um servidor falso `POST /v1/systemone` (`httptest`) e aponta o decider para ele. Nada sai da máquina e nenhum modelo é necessário.
- Com `LAYA_URL`, chama o endpoint real.
- `NewGuarded` mantém a execução segura: erro, timeout, id ausente ou resposta com baixa confiança mandam aquela pergunta para o `RuleDecider`. Aponte `LAYA_URL` para uma porta sem serviço para ver o fallback.

## Como aplicar

```go
d := compress.HTTPDecider{
    URL:    "http://localhost:8000/v1/systemone", // endpoint completo
    APIKey: os.Getenv("LAYA_API_KEY"),            // opcional, enviado como Bearer
    Model:  "multilingual",                       // opcional, por requisição
}
g := compress.NewGuarded(d) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Como executar

Offline (servidor falso):

```bash
go run ./examples/langgraph/decider_laya_http
```

Com um laya-serve real:

```bash
pip install "laya[serve]"
LAYA_MODELS=multilingual LAYA_DEVICE=cpu laya-serve   # endereço padrão 0.0.0.0:8000
```

O `laya-serve` escuta em `0.0.0.0:8000` por padrão. Fora de uma máquina confiável, defina `LAYA_HOST=127.0.0.1` e `LAYA_API_KEY=...`. `LAYA_MODELS` pré-carrega o modelo; a requisição indica o modelo no campo `model`, que este exemplo envia a partir de `LAYA_MODEL`. A forma `laya serve --model ...` aparece em algumas anotações, mas não está na documentação pública, por isso este exemplo não a usa.

Depois, em outro terminal:

```bash
LAYA_URL=http://localhost:8000/v1/systemone LAYA_MODEL=multilingual \
  go run ./examples/langgraph/decider_laya_http
# acrescente LAYA_API_KEY=... quando o servidor exigir
```

`LAYA_URL` é o endpoint completo, não uma URL base.

## Teste em modo de produção

1. Execute contra o servidor real com o modelo que você implanta e confira que cada resposta aparece como `choice` ou `noul`, e não como fallback.
2. Pare o servidor e execute de novo: todas as respostas devem cair nas regras e a execução deve terminar com código 0.
3. Defina um `Timeout` menor que o orçamento de latência do servidor no valor `Guarded`, e mantenha `Floor` em 0,7, a menos que você tenha medido um valor melhor.
4. Exponha o `laya-serve` apenas em rede privada, com `LAYA_API_KEY` definido. O exemplo envia a chave como `Authorization: Bearer`.

## Veja também

- [Middleware de compressão: `agents/middleware/compression.go`](../../../agents/middleware/compression.go)
- [decider_fallback](../decider_fallback/README.pt-BR.md): o decider por regras e o guard isoladamente
- [decider_laya_native](../decider_laya_native/README.pt-BR.md): as mesmas perguntas respondidas dentro do processo com laya-go
