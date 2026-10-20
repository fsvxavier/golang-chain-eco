# content_builder

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um deep agent que segue um arquivo de memória de voz da marca e uma skill para escrever um post de blog num diretório real.

## O que faz

- Cria um projeto temporário com `AGENTS.md` (voz da marca) e `skills/blog-post/SKILL.md`.
- `deepagents.Create` monta o diretório com `backends.Filesystem{Root: dir}` e carrega `Memory: ["/AGENTS.md"]` e `Skills: ["/skills/"]`.
- O agent lê a skill, escreve `/blog/go-concurrency.md`, e o exemplo imprime o arquivo do disco e confere que o system prompt trouxe a memória e o índice de skills.

## Como aplicar

Use arquivos de memória para regras que sempre valem e skills para procedimentos carregados sob demanda.

```go
deepagents.Create(deepagents.Options{
    Model: model, Backend: backends.Filesystem{Root: projectDir},
    Memory: []string{"/AGENTS.md"}, Skills: []string{"/skills/"},
})
```

Uma skill é uma pasta com um `SKILL.md` (frontmatter `name` e `description`); o agent só vê o índice até ler uma.

## Como rodar

```bash
go run ./examples/deepagents/content_builder
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use um modelo real e aponte `Root` para um diretório de projeto real, versionado (não um diretório temporário), para que toda mudança seja um diff revisável.
2. Rode numa branch, não na `main`: o agent escreve arquivos reais.
3. Mantenha `Root` o mais estreito possível; `Filesystem` confina os caminhos a ele, mas tudo dentro é gravável.
4. Revise o post gerado contra o checklist da skill (gancho, três pontos, call to action) e refine a skill quando o modelo pular uma etapa.
5. Mantenha segredos fora dos arquivos de memória e de skill; eles vão para o system prompt.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
