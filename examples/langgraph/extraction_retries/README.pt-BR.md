# extraction_retries

[English](README.md) | **Português** | [Español](README.es-ES.md)

Extração estruturada que valida o resultado e devolve os erros ao modelo até acertar.

## O que faz

- `extract` pede JSON `{name, age}`; `validate` faz unmarshal e chama `Person.Validate()`.
- O modelo roteirizado primeiro devolve `age: 311` (um erro de digitação). O erro de validação volta como mensagem humana e a segunda tentativa acerta. `graph.Sum()` conta as tentativas, com teto em 3.

## Como aplicar

```go
type Person struct{ Name string; Age int }
func (p Person) Validate() error { /* business rules */ }
// on error: append messages.Human("Validation failed: " + err.Error() + ". Try again.") and loop to extract
```

Coloque as regras em `Validate`, não no prompt: o modelo pode errar, o validador não. Para saída restrita, veja também `examples/langgraph/structured_output`.

## Como rodar

```bash
go run ./examples/langgraph/extraction_retries
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo real; adicione entradas realistas e ruidosas (erros de digitação, campos ausentes, duas pessoas numa frase).
2. Mantenha o limite de tentativas e decida o que acontece depois dele: devolver um erro, encaminhar para revisão humana ou guardar o texto bruto para depois.
3. Registre todo erro de validação; erros recorrentes apontam um problema de prompt ou de schema.
4. Valide também por segurança (limites de tamanho, valores permitidos) antes de os dados chegarem a um banco.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 4: Prompts, chains e parsers](../../../docs/pt-BR/manual/04-chains-prompts-parsers.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
