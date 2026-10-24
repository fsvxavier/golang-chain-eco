# structured_output

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent que termina com uma resposta tipada e validada em vez de texto livre. O agent roda com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail entra quando a conversa tem uma parcela real de código, e resultados JSON de tools são recodificados em TOON, tudo decidido por regras, sem precisar de modelo.

## O que faz

- `ResponseFormat` carrega um JSON Schema gerado da struct Go `WeatherReport` com `jsonschema.For[T]()`.
- Depois da chamada de tool, o agent devolve JSON que é interpretado em `state["structured_response"]` e impresso como valor tipado.

## Como aplicar

```go
agents.Create(agents.Options{
    Model: model, Tools: tools,
    ResponseFormat: &models.ResponseFormat{Name: "WeatherReport", Schema: jsonschema.For[WeatherReport]().Map()},
})
// out["structured_response"] holds the parsed object
```

Use sempre que outro programa, não uma pessoa, consome a resposta. Mantenha as tags da struct (`json`, `description`) corretas: elas são as instruções que o modelo vê.

## Como rodar

```bash
go run ./examples/langgraph/structured_output
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um provedor que suporte saída estruturada; confira se `structured_response` está presente em todo caminho, inclusive em erros de tool.
2. Valide a struct decodificada (faixas, enums) depois de decodificar; cumprir o schema não é correção de negócio. Veja `extraction_retries`.
3. Versione o schema; mudar um campo é uma mudança de API para todo consumidor.
4. Monitore falhas de parsing como uma taxa e alerte nos aumentos.

## Veja também

- [Manual, capítulo 4: Prompts, chains e parsers](../../../docs/pt-BR/manual/04-chains-prompts-parsers.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
