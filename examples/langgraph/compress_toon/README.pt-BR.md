# compress_toon

[English](README.md) | **Português** | [Español](README.es-ES.md)

Codifica um resultado de tool em JSON (uma lista de 20 usuários) como TOON e imprime as formas JSON e TOON, os tamanhos em bytes e em tokens estimados, e o fallback `TOONShorter` para um array pequeno que o TOON não encurtaria.

## O que faz

- Monta 20 linhas de usuários determinísticas e serializa para JSON, como um resultado de tool chegaria.
- `compress.TOONFromJSON(text, compress.TOONOptions{})` codifica o JSON em TOON. As linhas uniformes viram um bloco tabular: `users[20]{id,name,email,active,team}:` seguido de uma linha por registro.
- Compara os tamanhos com `messages.ApproxTokens` (bytes / 4 por mensagem) antes e depois.
- `compress.TOONShorter` devolve o texto TOON somente quando ele é estritamente menor. Para `[1]` mantém o JSON sem mudança.

O exemplo termina com erro se a forma TOON não for menor, ou se `TOONShorter` reescrever o valor pequeno. Não faz chamadas de rede.

## Como aplicar

Diretamente, no seu código:

```go
toon, err := compress.TOONFromJSON(resultJSON, compress.TOONOptions{})
if err == nil && len(toon) < len(resultJSON) {
    msg.Content = toon
}
```

Ou deixe o middleware de compressão fazer isso por chamada. O decider dele responde a pergunta `toon_tool_results`; quando a resposta é verdadeira, cada mensagem Tool sem `Parts` cujo conteúdo é JSON é substituída por `compress.TOONShorter(content)`. Os ids das chamadas de tool e a ordem das mensagens não mudam, e o estado armazenado não é alterado.

Use `TOONOptions{Delimiter: "|"}` ou `"\t"` apenas quando os valores contêm vírgulas. O delimitador é declarado no cabeçalho de cada array.

## Como executar

```bash
go run ./examples/langgraph/compress_toon
```

Saída esperada: o documento JSON (1716 bytes), o documento TOON (902 bytes), `bytes: 1716 -> 902 (saved 814)`, `estimated tokens: 432 -> 228` e a linha do fallback `rewritten: false, output: [1]`. A saída é a mesma em toda execução.

## Testar em modo de produção

1. Escolha um resultado real de tool que retorne uma lista de objetos com as mesmas chaves. Confira o tamanho TOON com `TOONFromJSON`. Resultados aninhados ou irregulares ganham menos, e arrays mistos caem na forma de lista.
2. Rode um provedor real com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` definida, uma vez com a resposta `toon_tool_results` do middleware forçada para `false` e outra para `true`. Compare os tokens de entrada informados pelo provedor. A estimativa aqui é só bytes / 4.
3. Confira se o modelo ainda responde corretamente a partir da forma TOON. Valores com `: " \ [ ] { }`, o delimitador, ou começando com `-` ou `#` são citados pelo encoder, então o modelo deve ver os valores sem alteração.
4. Rode `go test -race -count=1 ./compress` depois de mudar o encoder.

O formato TOON está especificado em https://github.com/toon-format/spec (spec v4.4). Este pacote implementa apenas o encoder, sem key folding nem decoder.

## Veja também

- [compress_history_summary](../compress_history_summary/README.pt-BR.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
