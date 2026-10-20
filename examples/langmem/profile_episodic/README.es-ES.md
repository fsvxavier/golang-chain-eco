# profile_episodic

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Las tres formas de memoria de largo plazo: una colección de hechos, un único perfil estricto y episodios de cómo se manejó una situación.

## Qué hace

- **Colección**: los hechos se acumulan, cambian y desaparecen. La segunda pasada actualiza "Likes dark mode" a "Dislikes dark mode" y borra "Lives in Rome" (`RemoveDoc`).
- **Perfil**: un documento con schema de struct Go (`UserProfile`), refinado en su sitio; las inserciones y borrados están desactivados y el ejemplo lo decodifica con `langmem.Decode[UserProfile]`.
- **Episódica**: `EpisodeSchema` guarda observación, pensamientos, acción y resultado, para que el agent reutilice un enfoque que funcionó.

## Cómo aplicarlo

```go
sm := langmem.NewStoreManager(model, nil, langmem.WithStore(st))                       // collection
pm := langmem.NewStoreManager(model, nil, langmem.WithStore(st),
    langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save preferences.")),
    langmem.WithInserts(false), langmem.WithDeletes(false),
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))                        // profile
em := langmem.NewStoreManager(model, nil, langmem.WithStore(st), langmem.WithSchemas(langmem.EpisodeSchema)) // episodes
```

Elige según la pregunta que harás después: "¿qué sé de este usuario?" (colección), "¿cuáles son las preferencias de este usuario?" (perfil), "¿cómo resolvimos algo parecido?" (episodios).

## Cómo ejecutarlo

```bash
go run ./examples/langmem/profile_episodic
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo real: el extractor debe seguir los schemas de las tools (`Memory`, `RemoveDoc`, tu struct de perfil); comprueba en conversaciones reales que actualiza en lugar de duplicar.
2. Usa un store duradero y un embedder real; da al manager un namespace de `user_id` tomado del usuario autenticado.
3. Valida los campos del perfil tras la extracción (longitud, valores permitidos) y ten presente la política de PII: ofrece a los usuarios leer, corregir y borrar.
4. Revisa una muestra de memorias cada semana; las malas extracciones se acumulan.

## Ver también

- [Manual, capítulo 16: Memoria a largo plazo (LangMem)](../../../docs/es-ES/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/es-ES/LANGMEM.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
