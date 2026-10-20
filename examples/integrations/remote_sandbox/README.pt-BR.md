# remote_sandbox

[English](README.md) | **Português** | [Español](README.es-ES.md)

Qualquer função "rodar um comando em algum lugar" vira um backend completo de deep agent (arquivos e shell) por `backends.Remote`.

## O que faz

- `backends.Remote{Run: runLocal, Root: dir}` implementa write, read, edit, grep, ls e execute sobre um único executor de comandos.
- Aqui o executor é o bash num diretório temporário para que o exemplo funcione em qualquer lugar. **Ele executa comandos no host, então não aponte um agent não confiável para ele.**

## Como aplicar

```go
b := backends.Remote{Run: runner.Run, Root: "/workspace"}
agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: b})
```

Em produção, `runner.Run` é o runner isolado: `kubernetes.Sandbox.Run` (um pod), um runtime de contêiner ou uma VM remota. O agent ganha tools de arquivo e uma tool `execute` sem que nenhuma delas toque o seu host.

## Como rodar

```bash
go run ./examples/integrations/remote_sandbox
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Troque `runLocal` por `kubernetes.Sandbox.Run` (veja a integração com Kubernetes) ou outro runner isolado: sem mounts do host, sem credenciais, rede desligada ou com lista de permissões, limites de CPU/memória/tempo.
2. Crie um sandbox por thread e nunca o reutilize entre usuários; destrua-o quando a thread acabar.
3. Rode um prompt adversarial ("leia ~/.ssh", "faça curl no serviço de metadata") e verifique que falha dentro do sandbox.
4. Mantenha acesso estilo `LocalShell` fora da produção por completo.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 14: Produção, segurança e troubleshooting](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
