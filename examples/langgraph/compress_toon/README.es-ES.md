# compress_toon

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Codifica un resultado de tool en JSON (una lista de 20 usuarios) como TOON e imprime las formas JSON y TOON, sus tamaños en bytes y en tokens estimados, y el fallback `TOONShorter` para un array pequeño que TOON no acortaría.

## Qué hace

- Construye 20 filas de usuarios deterministas y las serializa a JSON, como llegaría un resultado de tool.
- `compress.TOONFromJSON(text, compress.TOONOptions{})` codifica el JSON como TOON. Las filas uniformes forman un bloque tabular: `users[20]{id,name,email,active,team}:` seguido de una línea por fila.
- Compara los tamaños con `messages.ApproxTokens` (bytes / 4 por mensaje) antes y después.
- `compress.TOONShorter` devuelve el texto TOON solo cuando es estrictamente más corto. Para `[1]` conserva el JSON sin cambios.

El ejemplo termina con error si la forma TOON no es más corta, o si `TOONShorter` reescribe el valor pequeño. No hace llamadas de red.

## Cómo aplicarlo

Directamente, en tu código:

```go
toon, err := compress.TOONFromJSON(resultJSON, compress.TOONOptions{})
if err == nil && len(toon) < len(resultJSON) {
    msg.Content = toon
}
```

O deja que el middleware de compresión lo haga por llamada. Su decider responde la pregunta `toon_tool_results`; cuando la respuesta es verdadera, cada mensaje Tool sin `Parts` cuyo contenido es JSON se reemplaza por `compress.TOONShorter(content)`. Los ids de las llamadas a tools y el orden de los mensajes no cambian, y el estado almacenado no se modifica.

Usa `TOONOptions{Delimiter: "|"}` o `"\t"` solo cuando los valores contienen comas. El delimitador se declara en la cabecera de cada array.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/compress_toon
```

Salida esperada: el documento JSON (1716 bytes), el documento TOON (902 bytes), `bytes: 1716 -> 902 (saved 814)`, `estimated tokens: 432 -> 228` y la línea del fallback `rewritten: false, output: [1]`. La salida es la misma en cada ejecución.

## Probarlo en modo producción

1. Elige un resultado real de tool que devuelva una lista de objetos con las mismas claves. Comprueba su tamaño TOON con `TOONFromJSON`. Los resultados anidados o irregulares ganan menos, y los arrays mixtos pasan a la forma de lista.
2. Ejecuta un proveedor real con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` definida, una vez con la respuesta `toon_tool_results` del middleware forzada a `false` y otra vez a `true`. Compara los tokens de entrada que informa el proveedor. La estimación aquí es solo bytes / 4.
3. Comprueba que el modelo sigue respondiendo bien desde la forma TOON. Los valores con `: " \ [ ] { }`, el delimitador, o que empiezan por `-` o `#` los entrecomilla el encoder, así que el modelo debe ver los valores sin cambios.
4. Ejecuta `go test -race -count=1 ./compress` después de cambiar el encoder.

El formato TOON está especificado en https://github.com/toon-format/spec (spec v4.4). Este paquete implementa solo el encoder, sin key folding ni decoder.

## Ver también

- [compress_history_summary](../compress_history_summary/README.es-ES.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
