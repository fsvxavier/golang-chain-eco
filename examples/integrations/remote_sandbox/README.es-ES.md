# remote_sandbox

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Cualquier función "ejecutar un comando en algún lugar" se convierte en un backend completo de deep agent (archivos y shell) mediante `backends.Remote`.

## Qué hace

- `backends.Remote{Run: runLocal, Root: dir}` implementa write, read, edit, grep, ls y execute sobre un único ejecutor de comandos.
- Aquí el ejecutor es bash en un directorio temporal para que el ejemplo funcione en cualquier sitio. **Ejecuta comandos en el host, así que no apuntes un agent no confiable a él.**

## Cómo aplicarlo

```go
b := backends.Remote{Run: runner.Run, Root: "/workspace"}
agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: b})
```

En producción, `runner.Run` es el runner aislado: `kubernetes.Sandbox.Run` (un pod), un runtime de contenedores o una VM remota. El agent obtiene tools de archivo y una tool `execute` sin que ninguna toque tu host.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/remote_sandbox
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Cambia `runLocal` por `kubernetes.Sandbox.Run` (ver la integración con Kubernetes) u otro runner aislado: sin mounts del host, sin credenciales, red apagada o con lista de permitidos, límites de CPU/memoria/tiempo.
2. Crea un sandbox por thread y nunca lo reutilices entre usuarios; destrúyelo cuando el thread termine.
3. Ejecuta un prompt adversarial ("lee ~/.ssh", "haz curl al servicio de metadata") y verifica que falla dentro del sandbox.
4. Mantén el acceso estilo `LocalShell` fuera de producción por completo.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 14: Producción, seguridad y troubleshooting](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
