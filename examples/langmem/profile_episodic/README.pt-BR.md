# profile_episodic

[English](README.md) | **Português** | [Español](README.es-ES.md)

Os três formatos de memória de longo prazo: uma coleção de fatos, um único perfil estrito e episódios de como uma situação foi tratada.

## O que faz

- **Coleção**: fatos se acumulam, mudam e desaparecem. A segunda passada atualiza "Likes dark mode" para "Dislikes dark mode" e apaga "Lives in Rome" (`RemoveDoc`).
- **Perfil**: um documento com schema de struct Go (`UserProfile`), refinado no lugar; inserções e exclusões ficam desativadas e o exemplo o decodifica com `langmem.Decode[UserProfile]`.
- **Episódico**: `EpisodeSchema` guarda observação, pensamentos, ação e resultado, para que o agent reaproveite uma abordagem que funcionou.

## Como aplicar

```go
sm := langmem.NewStoreManager(model, nil, langmem.WithStore(st))                       // collection
pm := langmem.NewStoreManager(model, nil, langmem.WithStore(st),
    langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save preferences.")),
    langmem.WithInserts(false), langmem.WithDeletes(false),
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))                        // profile
em := langmem.NewStoreManager(model, nil, langmem.WithStore(st), langmem.WithSchemas(langmem.EpisodeSchema)) // episodes
```

Escolha pela pergunta que você fará depois: "o que sei sobre este usuário?" (coleção), "quais são as preferências deste usuário?" (perfil), "como resolvemos algo parecido?" (episódios).

## Como rodar

```bash
go run ./examples/langmem/profile_episodic
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo real: o extrator precisa seguir os schemas das tools (`Memory`, `RemoveDoc`, a sua struct de perfil); confira em conversas reais que ele atualiza em vez de duplicar.
2. Use um store durável e um embedder real; dê ao manager um namespace de `user_id` vindo do usuário autenticado.
3. Valide os campos do perfil depois da extração (tamanho, valores permitidos) e tenha a política de PII em mente: ofereça ler, corrigir e apagar aos usuários.
4. Revise uma amostra de memórias toda semana; extrações ruins se acumulam.

## Veja também

- [Manual, capítulo 16: Memória de longo prazo (LangMem)](../../../docs/pt-BR/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/pt-BR/LANGMEM.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
