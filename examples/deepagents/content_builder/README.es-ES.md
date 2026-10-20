# content_builder

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un deep agent que sigue un archivo de memoria de voz de marca y una skill para escribir un post de blog en un directorio real.

## Qué hace

- Crea un proyecto temporal con `AGENTS.md` (voz de marca) y `skills/blog-post/SKILL.md`.
- `deepagents.Create` monta el directorio con `backends.Filesystem{Root: dir}` y carga `Memory: ["/AGENTS.md"]` y `Skills: ["/skills/"]`.
- El agent lee la skill, escribe `/blog/go-concurrency.md`, y el ejemplo imprime el archivo del disco y comprueba que el system prompt llevó la memoria y el índice de skills.

## Cómo aplicarlo

Usa archivos de memoria para reglas que siempre aplican y skills para procedimientos que se cargan bajo demanda.

```go
deepagents.Create(deepagents.Options{
    Model: model, Backend: backends.Filesystem{Root: projectDir},
    Memory: []string{"/AGENTS.md"}, Skills: []string{"/skills/"},
})
```

Una skill es una carpeta con un `SKILL.md` (frontmatter `name` y `description`); el agent solo ve el índice hasta que lee una.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/content_builder
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa un modelo real y apunta `Root` a un directorio de proyecto real, versionado (no uno temporal), para que cada cambio sea un diff revisable.
2. Ejecuta en una rama, no en `main`: el agent escribe archivos reales.
3. Mantén `Root` lo más estrecho posible; `Filesystem` confina las rutas a él, pero todo lo de dentro es escribible.
4. Revisa el post generado contra el checklist de la skill (gancho, tres puntos, llamada a la acción) y afina la skill cuando el modelo se salte un paso.
5. Mantén los secretos fuera de los archivos de memoria y de skill; van al system prompt.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
