# compress_ponytail

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Añade la directiva Ponytail, un impulso a escribir menos código, al último mensaje Human de la copia usada en cada llamada, usando `middleware.NewCompression`.

## Qué hace

- Un historial de tres mensajes (Human, AI, Human) entra en `agents.Create` con el prompt de sistema `You are a Go developer working in this repository.`
- `middleware.NewCompression(decider)` envuelve el decider en `compress.NewGuarded`. El decider con guion responde sí a `ponytail` y elige un nivel (`lite`, `full` o `ultra`). Todas las demás preguntas responden no, así que solo cambia la nota Ponytail.
- La nota va después del último mensaje Human, una sola vez por llamada. El primer mensaje Human y el prompt de sistema quedan como estaban.
- Un middleware `sniff`, colocado después de la compresión, registra la petición que recibe el modelo. Un modelo fake responde sin conexión.
- El ejemplo imprime el tamaño de la nota y la primera línea de cada nivel, y falla si la nota no es el texto de la directiva, aparece dos veces, o llega al prompt de sistema o al historial almacenado.

La idea de la directiva viene de [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) (MIT). El texto en `compress/ponytail.go` es una reescritura original y más corta.

## Cómo aplicarlo

```go
agent, _ := agents.Create(agents.Options{
    Model:        model,
    SystemPrompt: "You are a Go developer working in this repository.",
    Middleware:   []agents.Middleware{middleware.NewCompression(decider)},
})
```

El decider debe responder `compress.QPonytail` (pregunta sí/no, `KindNoul`). Opcionalmente responde `compress.QLevel` para elegir `lite`, `full` (por defecto) o `ultra`. `compress.RuleDecider{}` responde sí a `ponytail` cuando los bloques de código ocupan al menos el 10% de los bytes de la conversación.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/compress_ponytail
```

Sin clave de API el ejemplo usa un modelo fake con guion, así que funciona sin conexión e imprime siempre la misma salida. Con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` definida, `examples/internal/demo` usa ese proveedor para la respuesta; la nota que recibe el modelo es la misma.

## Cómo probarlo en modo de producción

1. Sustituye el decider con guion por `compress.HTTPDecider` o un modelo pequeño; mantén `Floor` en `0.7`, para que una respuesta débil vuelva a las reglas.
2. Mide el efecto, no el texto: ejecuta las mismas tareas de código con y sin la nota y cuenta las líneas que añade el modelo, los archivos nuevos y las dependencias que introduce. Ponytail solo ayuda si el modelo lo sigue, y eso depende del modelo.
3. Comprueba que el modelo sigue validando entradas, manejando errores y escribiendo pruebas para la lógica de riesgo. La directiva dice que no recorte eso, pero confírmalo en tus tareas.
4. Empieza con `full`; usa `ultra` solo cuando las mediciones anteriores demuestren que ayuda.

## Ver también

- [Paquete compress](../../../compress/)
- [Ejemplo: compress_toon](../compress_toon/README.es-ES.md)
- [Ejemplo: compress_prompt_caveman](../compress_prompt_caveman/README.es-ES.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
