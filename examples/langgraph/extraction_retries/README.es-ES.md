# extraction_retries

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Extracción estructurada que valida el resultado y devuelve los errores al modelo hasta acertar.

## Qué hace

- `extract` pide JSON `{name, age}`; `validate` hace unmarshal y llama a `Person.Validate()`.
- El modelo guionizado primero devuelve `age: 311` (una errata). El error de validación vuelve como mensaje humano y el segundo intento acierta. `graph.Sum()` cuenta los intentos, con tope en 3.

## Cómo aplicarlo

```go
type Person struct{ Name string; Age int }
func (p Person) Validate() error { /* business rules */ }
// on error: append messages.Human("Validation failed: " + err.Error() + ". Try again.") and loop to extract
```

Pon las reglas en `Validate`, no en el prompt: el modelo puede equivocarse, el validador no. Para salida restringida, mira también `examples/langgraph/structured_output`.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/extraction_retries
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo real; añade entradas realistas y ruidosas (erratas, campos ausentes, dos personas en una frase).
2. Mantén el límite de intentos y decide qué ocurre después: devolver un error, derivar a revisión humana o guardar el texto en bruto para más tarde.
3. Registra cada error de validación; los errores recurrentes apuntan a un problema de prompt o de schema.
4. Valida también por seguridad (límites de tamaño, valores permitidos) antes de que los datos lleguen a una base de datos.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 4: Prompts, chains y parsers](../../../docs/es-ES/manual/04-chains-prompts-parsers.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
