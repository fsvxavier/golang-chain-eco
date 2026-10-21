# compress_shell_rtk

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Enruta un comando de shell por el filtro de salida `rtk` cuando un decider lo indica, y lo ejecuta con `backends.LocalShell`.

## Qué hace

- `compress.NewRTK(decider)` crea el enrutador. El decider de guion responde sí a `route_rtk`.
- `LocalShell.Rewrite` recibe `rtk.Rewrite`. La lista de permitidos (`echo`, `go`) comprueba el comando que pidió el llamador y después se aplica la reescritura, así que se imprime el comando realmente ejecutado.
- `rtk.Executor(shell)` registra el tamaño de la salida de cada ejecución; la siguiente decisión lo recibe como `prev_output_bytes`.
- Un comando rechazado (`curl`) lo bloquea `AllowedCommands` antes de cualquier reescritura.
- Si `rtk` no está en el `PATH`, el comando se ejecuta sin cambios y el ejemplo lo indica.

## Cómo aplicarlo

```go
rtk := compress.NewRTK(decider)               // decider: tu Decider, protegido por reglas
shell := backends.LocalShell{
    Filesystem:      backends.Filesystem{Root: dir},
    AllowedCommands: []string{"echo", "go"},
    Rewrite:         rtk.Rewrite,
    Quiet:           true,
}
res, err := rtk.Executor(shell).Execute(ctx, "go version", 30*time.Second)
```

Solo se reescriben comandos simples: pipes, `&&`, `;`, redirecciones, `$(...)`, comillas invertidas y `VAR=` al inicio pasan sin cambios.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/compress_shell_rtk
```

El ejemplo funciona sin conexión. Necesita `go` en el `PATH`; `rtk` es opcional.

## Cómo probarlo en modo producción

1. Instala `rtk` y confirma que `rtk --version` funciona, para ejercitar la ruta de reescritura.
2. Sustituye el decider de guion por `compress.HTTPDecider` o por un modelo real. `NewRTK` ya protege al decider con reglas, así que una respuesta lenta o errónea vuelve a las reglas.
3. Mantén `AllowedCommands` restringido. `LocalShell` se ejecuta en el host; usa un runner con sandbox para entradas no confiables.
4. Verifica que los comandos reescritos mantienen el código de salida esperado, ya que `rtk` solo filtra la salida.

## Ver también

- [Paquete compress](../../../compress/)
- [Ejemplo: compress_prompt_caveman](../compress_prompt_caveman/README.es-ES.md)
- [Manual, capítulo 5: Tools](../../../docs/es-ES/manual/05-tools.md)
