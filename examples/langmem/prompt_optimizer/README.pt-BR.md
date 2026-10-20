# prompt_optimizer

[English](README.md) | **Português** | [Español](README.es-ES.md)

Memória procedural: um system prompt que melhora sozinho a partir de conversas e feedback, e um otimizador de vários prompts que decide qual prompt causou uma falha.

## O que faz

- Uma trajetória (conversa mais feedback: o usuário queria código e recebeu teoria) vai para `langmem.NewPromptOptimizer` com a estratégia `Metaprompt`; ele reflete e reescreve o prompt.
- `MultiPromptOptimizer` faz atribuição de crédito entre vários prompts cooperantes (`research`, `explain`): classifica qual é o responsável e atualiza só ele.

## Como aplicar

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 2})
better, _ := opt.Optimize(ctx, trajectories, langmem.Prompt{Name: "main", Prompt: current})

mo := &langmem.MultiPromptOptimizer{Model: model, Kind: langmem.PromptMemory}
updated, _ := mo.Optimize(ctx, trajectories, prompts)
```

Colete trajetórias com notas ou comentários de usuários ou de evals, rode o otimizador offline e **revise** o novo prompt antes de implantá-lo.

## Como rodar

```bash
go run ./examples/langmem/prompt_optimizer
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo forte para a otimização e reúna trajetórias reais com feedback real (polegar, notas, scores de eval).
2. Nunca implante automaticamente: avalie o prompt candidato num dataset separado (veja `examples/evals/basic`) e compare com o atual.
3. Versione os prompts em controle de versão e guarde a trajetória que justificou cada mudança.
4. Rode periodicamente, não por requisição; a otimização é um job em lote.

## Veja também

- [Manual, capítulo 16: Memória de longo prazo (LangMem)](../../../docs/pt-BR/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/pt-BR/LANGMEM.md)
- [EVALS.md](../../../docs/pt-BR/EVALS.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
