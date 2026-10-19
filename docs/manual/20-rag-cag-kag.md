# 20. RAG, CAG e KAG

Três jeitos de dar conhecimento a um modelo, com a mesma interface: `knowledge.Answerer` recebe uma pergunta e devolve texto, fontes, estratégia usada e um rastro (`Trace`) das decisões.
Este capítulo mostra quando usar cada um, como montar, como deixar o roteador escolher e como ligar tudo a agents e MCP.
Referência dos tipos: [KNOWLEDGE.md](../KNOWLEDGE.md). Desenho: [DESIGN-RAG-CAG-KAG.md](../../DESIGN-RAG-CAG-KAG.md).

## 20.1 Qual usar

| | RAG | CAG | KAG |
|---|---|---|---|
| Ideia | busca os trechos mais próximos e injeta no prompt | coloca o corpus inteiro no prompt e deixa o provedor cachear | extrai um grafo de entidades e relações e raciocina por saltos |
| Pacote | `rag` | `cag` | `kag` |
| Escala de dados | alta (milhões de trechos) | baixa (cabe na janela do modelo) | média a alta (depende da modelagem) |
| Latência por pergunta | média | baixa (prefixo em cache) | maior (plano, saltos, síntese) |
| Pergunta multi-hop | fraca se a resposta estiver em trechos separados | boa, o modelo vê tudo | melhor caso |
| Atualizar dados | fácil (reindexar a fonte) | troca o corpus e perde o cache | reextrair os trechos alterados |
| Custo por pergunta | médio | baixo com cache, alto sem | mais chamadas ao modelo |

Regra prática: milhares de documentos ou base que muda sempre: **RAG**. Poucos documentos estáveis e latência mínima: **CAG**. Perguntas do tipo "quem se relaciona
com quem" ou "qual a causa raiz": **KAG**. Na dúvida, `knowledge.Router` decide por você (seção 20.5).

## 20.2 RAG

```go
emb := embeddings.Fake{Dim: 256} // troque por um embedder real
store := vectorstores.NewMemory(emb) // ou qualquer VectorStore de integrations/vectorstore

idx := rag.NewIndexer(store, nil) // nil = textsplitters.NewRecursive(800, 100)
if _, err := idx.Index(ctx, documents.Directory("docs", "*.md", nil)); err != nil { /* ... */ }

p := &rag.Pipeline{
    Store: store, Docs: idx.Documents(), Model: model,
    K: 4, Hybrid: true, // vetor + BM25 por RRF
}
ans, err := p.Answer(ctx, knowledge.Question{Text: "Como redefinir a senha do Router-X500?"})
fmt.Println(ans.Text)
for _, s := range ans.Sources { fmt.Println(s.Metadata["source"]) }
```

- A resposta cita as fontes como `[1]`, `[2]`, na ordem de `ans.Sources`.
- `Hybrid` acha termos exatos (códigos de erro, nomes de produto) que a busca vetorial perde. Veja `go run ./examples/knowledge/rag`.
- Reindexar é barato: uma fonte com o mesmo conteúdo é pulada (`Skipped`); uma alterada troca só os seus trechos.
- Quer reordenar com um modelo de rerank? Preencha `Reranker` com o resultado de `chatmodels.NewReranker("cohere:<modelo>")` (também `jina:` e `voyageai:`).
- Filtrar por metadado: `knowledge.Question{Filter: vectorstores.MetadataEquals(map[string]any{"produto": "x500"})}`.

Combine com qualquer backend vetorial do capítulo 13 (pgvector, Qdrant, Milvus, Chroma, Elasticsearch, Neo4j, Memgraph...): o `Pipeline` só vê um `vectorstores.VectorStore`.

## 20.3 CAG

```go
corpus := cag.NewCorpus(docs) // a ordem de entrada não importa
e := &cag.Engine{
    Model:    chatmodels.MustInit("anthropic:claude-sonnet-5-5"),
    Corpus:   corpus,
    Window:   200000,
    Fallback: p, // o pipeline RAG acima, usado se o corpus não couber
}
ans, err := e.Answer(ctx, knowledge.Question{Text: "Qual a multa de rescisão com o Fornecedor Y?"})
```

- O corpus vai na mensagem `system` e o histórico depois, para o prefixo ficar idêntico entre perguntas e o cache do provedor acertar.
- Anthropic: o bloco recebe `cache_control: ephemeral`. OpenAI e compatíveis: cache automático por prefixo. Outros provedores funcionam, sem economia.
- Para confirmar que o cache funcionou, olhe `ans.Usage.CacheReadTokens` ou o passo `cache_hit` no `Trace`.
- Corpus alterado: `e.SetCorpus(novo)` devolve `true` e o cache do provedor será reconstruído na próxima chamada.
- Corpus acima de `Window - Reserve`: usa o `Fallback`; sem `Fallback`, `cag.ErrCorpusTooLarge`.

A contagem de tokens é uma estimativa (~4 caracteres por token). Perto do limite, injete um `knowledge.TokenCounter` próprio em `Engine.Counter`.

## 20.4 KAG

```go
g := kag.NewMemory() // ou &graphstore.Neo4j{...} / &graphstore.Memgraph{...} para um banco de grafo

x := &kag.Extractor{Model: model, Embedder: emb, Graph: g,
    Schema: kag.Schema{RelationTypes: []string{"PRESCREVEU", "INTERAGE_COM", "PRESCRITA_A"}}}
stats, err := x.Extract(ctx, chunks) // chunks: []documents.Document

e := &kag.Engine{Model: model, Graph: g, Embedder: emb, Chunks: p, Verify: true}
ans, err := e.Answer(ctx, knowledge.Question{
    Text: "Quem prescreveu um remédio que interage com a Aspirina prescrita a João?"})
for _, st := range ans.Trace { fmt.Println(st.Name, st.Detail) } // uma linha por salto
```

- O extrator transforma cada trecho em entidades e relações; cada tripla guarda o trecho de origem. Vazios e duplicatas são descartados.
- O engine planeja a pergunta em passos, percorre o grafo (sem repetir triplas, mesmo com ciclos) e sintetiza com a evidência numerada. As fontes da resposta são as triplas usadas.
- `Verify: true` confere a resposta contra a evidência e marca `(não verificado)` quando algo não é sustentado.
- `Chunks` (um `Answerer` de RAG) completa passos em que o grafo não tem nada.
- Use `Schema.RelationTypes` para o vocabulário de relações ficar estável; o tipo é normalizado para `MAIÚSCULAS_COM_UNDERSCORE` antes de ir ao grafo.

`go run ./examples/knowledge/kag` mostra a busca vetorial sozinha falhando na pergunta de 3 saltos e o engine respondendo.

## 20.5 Roteador

```go
r := &knowledge.Router{
    RAG: p, CAG: e1, KAG: e2,
    Profile:  knowledge.Profile{CorpusTokens: 40000, ContextWindow: 200000, Volatile: false, HasGraph: true},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: "Quem se relaciona com o contrato Y?"})
fmt.Println(ans.Strategy, ans.Trace[0]) // kag, route: kag — pergunta multi-hop com grafo disponível
```

O primeiro passo do `Trace` é sempre a decisão (`route`). Dá para forçar a técnica por pergunta com `Question.Force`. Ordem das regras: Force, depois multi-hop com grafo (KAG),
depois corpus estável que cabe em 80% da janela (CAG), senão RAG. Com `Fallback: true`, erro em CAG ou KAG repete a pergunta no RAG, e o `Trace` registra isso.
O classificador de multi-hop padrão é uma heurística de palavras (pt-BR e en); use `knowledge.LLMClassifier{Model: m}` quando precisar de mais precisão.

## 20.6 Receitas

- **Cada técnica atrás de uma interface só.** Código de aplicação recebe um `knowledge.Answerer`; trocar RAG por CAG não muda mais nada.
- **Como chain/Runnable:** `knowledge.AsRunnable(a)` devolve `runnables.Runnable[Question, Answer]`.
- **Medir economia de cache:** some `Usage.CacheReadTokens` das respostas; `InputTokens` continua somando tudo.
- **Persistir:** `vectorstores.Memory.Save/Load` e `kag.Memory.Save/LoadMemory` gravam em JSON; em produção use um backend vetorial real (capítulo 13).

## 20.7 Limites conhecidos

- A indexação incremental do RAG guarda os hashes só na memória do processo; reiniciar reindexa tudo. Fontes que somem do loader não são removidas do store.
- O BM25 do pipeline é recriado a cada pergunta (bom até alguns milhares de trechos).
- O CAG depende do cache do provedor, que expira e cobra a primeira escrita; a contagem de tokens é estimada.
- No KAG, a qualidade do plano e da extração vem do modelo. Um passo cujas triplas já foram coletadas por outro pode acionar o complemento por chunks sem necessidade.
- O `Usage` do KAG cobre só a chamada de síntese (plano, extração e verificação não entram).

## 20.8 Problemas comuns

| Sintoma | Causa provável |
|---|---|
| `cag: corpus does not fit the context window` | corpus maior que `Window - Reserve`; defina `Fallback` ou reduza o corpus |
| `Cache read` sempre zero no Anthropic | prefixo mudou entre chamadas (corpus alterado, instrução diferente) ou o cache expirou |
| KAG responde "não sei" | grafo vazio ou extração sem triplas (veja `ExtractStats.Dropped`); defina `Chunks` como complemento |
| `rag: cannot answer with strategy "kag"` | `Question.Force` aponta para outra técnica; deixe o roteador decidir ou limpe o campo |
| `knowledge: no strategy available` | `Router` sem nenhuma técnica configurada, ou `Force` para uma técnica `nil` |

Próximo: volta ao [índice](README.md).
