# hot_path

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent que cura a própria memória de longo prazo com as tools `manage_memory` e `search_memory`, isolada por usuário. O agent roda com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail entra quando a conversa tem uma parcela real de código, e resultados JSON de tools são recodificados em TOON, tudo decidido por regras, sem precisar de modelo.

## O que faz

- `langmem.NewIndexedStore` adiciona busca semântica a um store chave-valor; o namespace da memória é `["memories", "{langgraph_user_id}"]`, então cada usuário tem um espaço privado.
- O modelo roteirizado salva o que a Alice pede para lembrar e depois recupera. Fazer a mesma pergunta como Bob não encontra nada: as memórias não vazam entre usuários.

## Como aplicar

```go
st := langmem.NewIndexedStore(store.NewMemory(), embedder)
cfg := langmem.ToolConfig{Namespace: langmem.Namespace{"memories", "{langgraph_user_id}"}}
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
    Compile: []graph.CompileOption{graph.WithStore(st)},
})
ctx = langmem.WithConfigurable(ctx, "langgraph_user_id", userID)
```

"Hot path" significa que o agent decide lembrar durante a conversa, ao custo de uma chamada de tool na latência do usuário.

## Como rodar

```bash
go run ./examples/langmem/hot_path
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use um modelo e um embedder reais e um store durável (os stores SQL, Redis, MongoDB, DynamoDB e S3 implementam `store.Store`).
2. Defina `langgraph_user_id` a partir do usuário autenticado, nunca do corpo da requisição; teste que o usuário B não lê as memórias do usuário A.
3. Decida o que o agent pode guardar (sem segredos, sem categorias sensíveis sem consentimento) e ofereça uma forma de listar e apagar as memórias de um usuário.
4. Acompanhe a quantidade de memórias por usuário e pode ou consolide; veja `background` para formar memórias fora do hot path.

## Veja também

- [Manual, capítulo 16: Memória de longo prazo (LangMem)](../../../docs/pt-BR/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/pt-BR/LANGMEM.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
