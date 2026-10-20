# basic

[English](README.md) | **Português** | [Español](README.es-ES.md)

O menor deep agent: ferramentas de planejamento, filesystem virtual e delegação a subagents prontos para uso.

## O que faz

- Cria um deep agent com um system prompt e um modelo que escreve `/notes.md` pela tool embutida `write_file`.
- O arquivo vive no estado do agent (filesystem virtual), não em disco; o exemplo o imprime com `backends.FilesFromState`.

## Como aplicar

`deepagents.Create` dá ao agent `write_todos`, as tools de arquivo (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`) e uma tool `task` para subagents, sem nenhuma ligação extra.

```go
agent, _ := deepagents.Create(deepagents.Options{Model: model, SystemPrompt: "You are a research assistant."})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Save a note about deep agents."})
files := backends.FilesFromState(out)
```

Comece por aqui em tarefas longas que precisam de um rascunho; adicione `Backend`, `SubAgents`, `Memory` e `Skills` conforme a tarefa cresce (os outros exemplos de deepagents).

## Como rodar

```bash
go run ./examples/deepagents/basic
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Rode com uma chave real; agora o modelo escolhe se escreve o arquivo, então confira se `/notes.md` existe e faz sentido.
2. Escolha o backend com cuidado: o backend de estado padrão some com a execução. Use `backends.Filesystem{Root: dir}` para arquivos reais (confinados a `dir`) ou `backends.Remote` para um runner isolado.
3. Nunca exponha `backends.LocalShell` a entrada não confiável; use `kubernetes.Sandbox` ou outro runner isolado.
4. Adicione um checkpointer e um `thread_id` para que execuções longas sobrevivam a reinícios, e defina um limite de recursão explícito.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Manual, capítulo 14: Produção, segurança e troubleshooting](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
