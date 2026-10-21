# compress_ponytail

[English](README.md) | **Português** | [Español](README.es-ES.md)

Acrescenta a diretiva Ponytail, um incentivo a escrever menos código, à última mensagem Human da cópia usada em cada chamada, usando `middleware.NewCompression`.

## O que faz

- Um histórico de três mensagens (Human, AI, Human) entra em `agents.Create` com o prompt de sistema `You are a Go developer working in this repository.`
- `middleware.NewCompression(decider)` envolve o decider em `compress.NewGuarded`. O decider roteirizado responde sim para `ponytail` e escolhe um nível (`lite`, `full` ou `ultra`). Todas as outras perguntas respondem não, então só a nota Ponytail muda.
- A nota vai depois da última mensagem Human, só uma vez por chamada. A primeira mensagem Human e o prompt de sistema continuam como estavam.
- Um middleware `sniff`, posto depois da compressão, registra a requisição que o modelo recebe. Um modelo fake responde offline.
- O exemplo imprime o tamanho da nota e a primeira linha de cada nível, e falha se a nota não for o texto da diretiva, se aparecer duas vezes, ou se chegar ao prompt de sistema ou ao histórico armazenado.

A ideia da diretiva vem de [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) (MIT). O texto em `compress/ponytail.go` é uma reescrita original e mais curta.

## Como aplicar

```go
agent, _ := agents.Create(agents.Options{
    Model:        model,
    SystemPrompt: "You are a Go developer working in this repository.",
    Middleware:   []agents.Middleware{middleware.NewCompression(decider)},
})
```

O decider deve responder `compress.QPonytail` (pergunta sim/não, `KindNoul`). Opcionalmente responda `compress.QLevel` para escolher `lite`, `full` (padrão) ou `ultra`. `compress.RuleDecider{}` responde sim para `ponytail` quando blocos de código ocupam pelo menos 10% dos bytes da conversa.

## Como rodar

```bash
go run ./examples/langgraph/compress_ponytail
```

Sem chave de API o exemplo usa um modelo fake roteirizado, então roda offline e imprime a mesma saída sempre. Com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` definida, o `examples/internal/demo` usa esse provedor para a resposta; a nota que o modelo recebe é a mesma.

## Como testar em modo de produção

1. Troque o decider roteirizado por `compress.HTTPDecider` ou um modelo pequeno; mantenha `Floor` em `0.7`, para que uma resposta fraca volte para as regras.
2. Meça o efeito, não o texto: rode as mesmas tarefas de código com e sem a nota e conte as linhas que o modelo acrescenta, os arquivos novos e as dependências introduzidas. O Ponytail só ajuda se o modelo seguir a diretiva, e isso depende do modelo.
3. Confira que o modelo continua validando entradas, tratando erros e escrevendo testes para lógica arriscada. A diretiva diz para não cortar isso, mas confirme nas suas tarefas.
4. Comece com `full`; use `ultra` só depois que as medições acima mostrarem que ajuda.

## Veja também

- [Pacote compress](../../../compress/)
- [Exemplo: compress_toon](../compress_toon/README.pt-BR.md)
- [Exemplo: compress_prompt_caveman](../compress_prompt_caveman/README.pt-BR.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
