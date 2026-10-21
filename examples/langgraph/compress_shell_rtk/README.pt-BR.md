# compress_shell_rtk

[English](README.md) | **Português** | [Español](README.es-ES.md)

Roteia um comando de shell pelo filtro de saída `rtk` quando um decider decide que deve, e o executa com `backends.LocalShell`.

## O que faz

- `compress.NewRTK(decider)` monta o roteador. O decider roteirizado responde sim para `route_rtk`.
- `LocalShell.Rewrite` recebe `rtk.Rewrite`. A allowlist (`echo`, `go`) verifica o comando pedido pelo chamador, depois a reescrita é aplicada, então o comando executado é impresso.
- `rtk.Executor(shell)` registra o tamanho da saída de cada execução; a decisão seguinte recebe isso como `prev_output_bytes`.
- Um comando recusado (`curl`) é rejeitado por `AllowedCommands` antes de qualquer reescrita.
- Se `rtk` não estiver no `PATH`, o comando roda sem alteração e o exemplo avisa.

## Como aplicar

```go
rtk := compress.NewRTK(decider)               // decider: seu Decider, protegido pelas regras
shell := backends.LocalShell{
    Filesystem:      backends.Filesystem{Root: dir},
    AllowedCommands: []string{"echo", "go"},
    Rewrite:         rtk.Rewrite,
    Quiet:           true,
}
res, err := rtk.Executor(shell).Execute(ctx, "go version", 30*time.Second)
```

Só comandos simples são reescritos: pipes, `&&`, `;`, redirecionamentos, `$(...)`, crases e `VAR=` no início passam sem alteração.

## Como rodar

```bash
go run ./examples/langgraph/compress_shell_rtk
```

O exemplo roda offline. Precisa do `go` no `PATH`; o `rtk` é opcional.

## Como testar em modo de produção

1. Instale o `rtk` e confirme que `rtk --version` funciona, para que o caminho de reescrita seja exercitado.
2. Troque o decider roteirizado por `compress.HTTPDecider` ou por um modelo real. O `NewRTK` já protege o decider com regras, então uma resposta lenta ou errada volta para as regras.
3. Mantenha `AllowedCommands` restrito. O `LocalShell` roda no host; use um runner com sandbox para entrada não confiável.
4. Confira se os comandos reescritos mantêm o código de saída esperado, já que o `rtk` filtra apenas a saída.

## Veja também

- [Pacote compress](../../../compress/)
- [Exemplo: compress_prompt_caveman](../compress_prompt_caveman/README.pt-BR.md)
- [Manual, capítulo 5: Tools](../../../docs/pt-BR/manual/05-tools.md)
