# Design: RAG, CAG e KAG no golang-chain-eco

Status: **aprovado pelo solicitante (decisões da seção 8 fechadas)**. Nada foi implementado ainda. Origem: [rag_cag_kag.md](rag_cag_kag.md).
Itens marcados com **(decisão minha)** são escolhas do autor do design, não do solicitante.

## 1. Intenção

Hoje o repositório cobre RAG só em peças soltas, tem apenas o marcador `cache_control` para CAG e não tem grafo de conhecimento real
para KAG. O objetivo é oferecer as três técnicas de aumento de contexto como implementações **completas e agnósticas**: o mesmo núcleo
roda com qualquer `models.ChatModel`, `embeddings.Embedder`, `vectorstores.VectorStore` e `documents.Loader` do projeto, sem amarrar a
um caso de uso. Além do núcleo, entram integrações (tools para agentes, servidor MCP, adapters de armazenamento de vetor e de grafo
para bancos reais), exemplos que rodam offline e um manual.

Estado atual aproveitado:

| Peça existente | Onde | Papel no design |
|---|---|---|
| Retriever, `FromVectorStore`, `Ensemble` (RRF), `MultiQuery`, `AsTool` | `core/retrievers` | base do RAG |
| BM25 e `WithRerank` | `integrations/retrieval` | busca híbrida e rerank |
| `VectorStore` em memória e adapters (`Backend`: `Upsert/Query/Delete`) | `core/vectorstores`, `integrations/vectorstore` | armazenamento vetorial |
| Loaders, splitters, embeddings (`Fake`, `Cached`) | `core/documents`, `core/textsplitters`, `core/embeddings` | indexação |
| Graph RAG por metadados | `integrations/graphretriever` | ponto de partida do KAG, não substituído |
| `cache_control` em `ContentPart.Extra`, tokens de cache lidos da resposta | `providers/anthropic`, `core/messages` | base do CAG |
| Cliente Bolt (Memgraph) e Neo4j via Query API HTTP | `integrations/memgraph`, `integrations/vectorstore/neo4j.go` | adapters de grafo |
| `ciam.Toolkit` com `All()` e `MCPServer(...)` | `ciam/tools.go` | padrão a seguir nas tools e no MCP |
| `models.Fake`, `examples/internal/demo` | `core/models`, `examples/internal` | exemplos offline |

## 2. Objetivos, não objetivos e anti-objetivos

**Objetivos**

- Três técnicas completas atrás de uma interface comum, intercambiáveis e combináveis por um roteador.
- Agnóstico: nenhuma dependência de provedor, banco ou domínio no núcleo.
- Integrações: tools para agentes, servidor MCP, adapters de grafo e de vetor para bancos reais.
- Exemplos offline por técnica, testes, manual em pt-BR e atualização dos índices (READMEs e contagens).

**Não objetivos**

- Não reimplementar o que já existe (retrievers, BM25, vector stores, splitters). O novo código compõe essas peças.
- Não criar um mecanismo de KV cache local de LLM. CAG aqui depende do cache de prompt do provedor.
- Não escrever um motor de grafo próprio de alto desempenho; o grafo em memória serve a testes, exemplos e bases pequenas.
- Não quebrar API pública existente.

**Anti-objetivos (falha mesmo que "funcione")**

- Código que só roda com um provedor ou só com um banco.
- Qualquer import de módulo de terceiros em código de biblioteca (regra do CLAUDE.md: só stdlib).
- Exemplos que exigem rede ou chave de API para rodar.
- Respostas sem rastreio de fonte: toda resposta devolve de onde veio.

## 3. Restrições

- Go 1.27, módulo `github.com/fsvxavier/golang-chain-eco`, **só stdlib** em pacotes de produção.
- Exemplos rodam offline com modelo fake; provedor real só se `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` estiver definida.
- Testes de serviços reais pulam sozinhos sem a variável de ambiente (padrão `internal/conformance`).
- Ao adicionar pacote ou exemplo, atualizar o mapa de pacotes e as contagens em `README.md`, `docs/ARCHITECTURE.md`,
  `docs/manual/README.md` e `examples/README.md`. Docs em pt-BR.
- Schemas de tools validam listas como `[]any`; ids de tool call são únicos na conversa.

## 4. Arquitetura

Novos pacotes de topo, na camada acima de `core/` e `integrations/`:

```
knowledge/        interface comum, tipos de resposta, roteador, tools e MCP
rag/              indexador + pipeline de busca híbrida
cag/              corpus em prefixo de prompt cacheável
kag/              grafo de conhecimento: extração, planejamento, solver, verificação
integrations/graphstore/    adapters de GraphStore (Neo4j HTTP, Memgraph Bolt)
integrations/vectorstore/   novos adapters de vetor quando faltarem (ver 4.6)
examples/knowledge/{rag,cag,kag,router,agent_tools,mcp_server}/
docs/manual/20-rag-cag-kag.md
```

### 4.1 Contrato comum (`knowledge`)

```go
type Answerer interface {
    Answer(ctx context.Context, q Question) (Answer, error)
}

type Question struct {
    Text    string
    History []messages.Message // multi-turno opcional
    Filter  vectorstores.Filter
}

type Answer struct {
    Text     string
    Sources  []Source        // documento/trecho, id, metadados, score
    Strategy string          // "rag" | "cag" | "kag"
    Usage    *messages.Usage
    Trace    []Step          // passos do raciocínio (KAG) ou da recuperação (RAG)
}
```

Cada técnica implementa `Answerer`, expõe um `runnables.Runnable[Question, Answer]` e pode ser usada como `tools.Tool`.
O modelo é sempre injetado (`models.ChatModel`); o prompt de síntese é um template substituível.

### 4.2 RAG (`rag`)

- **Indexador**: `Loader → Splitter → Embedder → VectorStore`, com `Cached` para embeddings, metadados de proveniência
  (`source`, `chunk`, `offset`) e re-indexação incremental por hash de conteúdo.
- **Pipeline**: busca vetorial + BM25 fundidas por RRF (`retrievers.Ensemble`), `MultiQuery` opcional, filtro de metadados, MMR,
  rerank opcional (`retrieval.WithRerank`), corte por orçamento de tokens, resposta com citações.
- Reaproveita `core/retrievers` e `integrations/retrieval`; o pacote adiciona a orquestração, o prompt com citação e o indexador.

### 4.3 CAG (`cag`)

- **Corpus**: carrega documentos via `Loader`, ordena de forma determinística e monta um **prefixo estável** (mesmos bytes a cada
  chamada, condição para o cache de prompt acertar).
- **Provedor-agnóstico (decisão minha)**: uma interface pequena `PrefixCacher` decide como marcar o prefixo. Para Anthropic coloca
  `cache_control: {type: ephemeral}` no último bloco do prefixo; para providers com cache automático por prefixo (OpenAI-compatíveis)
  apenas garante a estabilidade do prefixo; para os demais funciona sem cache, com o aviso de custo no `Trace`.
- **Orçamento**: estima tokens do corpus (contador substituível; padrão heurístico, pois o repo não tem tokenizer) contra a janela do
  modelo. Se não couber: erro explícito ou **fallback para RAG** configurável.
- **Invalidação**: hash do corpus; mudou, o prefixo é reconstruído e o `Answer` informa a troca.
- **Métricas**: expor tokens lidos/escritos de cache. Hoje `anthropic.toUsage` soma tudo em `InputTokens`; será preciso expor os
  campos de cache em `messages.Usage` (campos novos, sem quebrar os existentes). Aprovado (decisão 1).
- Multi-turno: histórico vai depois do prefixo, nunca antes.

### 4.4 KAG (`kag`)

- **Modelo de dados**: `Entity{ID, Type, Name, Props}`, `Relation{From, Type, To, Props, Source}`.
- **`GraphStore`** (interface): `UpsertEntities`, `UpsertRelations`, `Neighbors(id, relTypes, dir)`, `FindEntities(text|vector, k)`,
  `Path(from, to, maxHops)`, `Delete`. Implementação em memória completa (índice por entidade e por tipo de relação, busca por
  nome e por vetor via `Embedder`).
- **Construção**: extrator por LLM com saída estruturada (`models.Structured`) que tira entidades e triplas de cada chunk, resolve
  duplicatas (normalização + similaridade) e liga cada relação ao chunk de origem (proveniência). Opcional: esquema/ontologia
  fornecido pelo usuário para restringir tipos.
- **Consulta (solver)**: (1) o planejador decompõe a pergunta em passos encadeados (multi-hop); (2) cada passo faz entity linking
  (vetor + nome), percorre o grafo e, quando o grafo não basta, completa com busca vetorial nos chunks de origem; (3) os resultados
  alimentam o passo seguinte; (4) síntese final com as triplas usadas como evidência.
- **Verificação**: uma etapa confere se cada afirmação da resposta tem tripla ou chunk de suporte; sem suporte, marca ou reexecuta.
  Limite de passos e de saltos configurável.
- `integrations/graphretriever` continua existindo para o caso simples baseado em metadados.

### 4.5 Roteador (`knowledge.Router`)

Escolhe a técnica pela matriz do documento original: corpus que cabe na janela e muda pouco → CAG; pergunta com relações e
multi-hop e grafo disponível → KAG; demais casos → RAG. Regras explícitas e configuráveis (tamanho do corpus, taxa de mudança,
presença de grafo, classificação da pergunta por LLM opcional). Aceita forçar a técnica por chamada e combinar (ex.: KAG com
fallback para RAG). Toda decisão fica registrada em `Answer.Trace`.

### 4.6 Integrações

**Tools para agentes**: `knowledge.Toolkit` no formato do `ciam.Toolkit`: `All() []tools.Tool` com `rag_search`, `rag_answer`,
`cag_answer`, `kag_query`, `kag_explain_path`, `knowledge_ingest` (RAG e KAG), usando `tools.New[Args]`. Retorno via
`tools.ContentAndArtifact` (texto + fontes). Funciona com `agents` e `deepagents`.

**MCP**: `Toolkit.MCPServer(onCall)` reutiliza `mcp/server`, servindo as mesmas tools a qualquer cliente MCP (stdio ou HTTP).
Opcionalmente um subcomando `cmd/knowledge` para indexar e consultar pela linha de comando (incluído por decisão do solicitante, na fase de integrações).

**Adapters de grafo (`integrations/graphstore`)**: implementam `kag.GraphStore`.
- Neo4j via Query API HTTP (mesmo caminho do adapter vetorial existente), Cypher parametrizado, rótulos validados por regex.
- Memgraph via o cliente Bolt de `integrations/memgraph`.
- Ambos em Cypher; testes de contrato com `httptest` e, quando houver env var, contra o serviço real, usando o padrão de
  `internal/conformance`. Outros bancos de grafo ficam fora do escopo inicial.

**Adapters de vetor**: o repositório já tem pgvector, Qdrant, Milvus, Pinecone, Weaviate, Chroma, Elasticsearch, Neo4j, Memgraph e
outros via `Backend`. O RAG e o KAG consomem qualquer `VectorStore`, então **não há adapter novo obrigatório**. O trabalho aqui é a
suíte de conformidade do RAG rodando sobre os backends já existentes e a documentação de qual combinar com qual.

### 4.7 Observabilidade e persistência

- `callbacks.Start` em cada etapa (recuperação, extração, passo do plano) para que `tracing` e o stream do LangGraph enxerguem.
- Grafo e índice são persistíveis pelos stores; o corpus do CAG é reconstruído do `Loader` (sem estado próprio além do hash).
- Expor RAG/CAG/KAG como nós de `langgraph/graph` (função auxiliar) para uso em fluxos, sem acoplar o núcleo ao LangGraph.

## 5. Entregáveis

1. Pacotes `knowledge`, `rag`, `cag`, `kag` com testes unitários (`go test -race`).
2. `integrations/graphstore` (Neo4j, Memgraph) com testes de contrato e testes de serviço real que se pulam sozinhos.
3. `knowledge.Toolkit` + servidor MCP, com teste de ponta a ponta usando cliente MCP do repo.
4. Exemplos offline: `rag`, `cag`, `kag`, `router`, `agent_tools`, `mcp_server`, cada um com modelo fake e dados embutidos.
5. Manual `docs/manual/20-rag-cag-kag.md` (pt-BR): conceito, quando usar cada um (matriz do documento original), receitas,
   integrações, limites conhecidos. Atualização de `README.md`, `docs/ARCHITECTURE.md`, `docs/manual/README.md`,
   `examples/README.md`, `docs/INTEGRATIONS.md`.
6. `go build ./...`, `go vet ./...` e `go test -race -count=1 ./...` verdes.

## 6. Critérios de aceite

- Mesmo conjunto de testes do RAG passa com `Memory`, `integrations/vectorstore.Local` e um backend `httptest`.
- CAG: segunda chamada com o mesmo corpus mantém prefixo idêntico byte a byte (teste); mudança no corpus muda o hash;
  corpus acima do orçamento aciona o fallback ou o erro configurado.
- KAG: pergunta multi-hop de dois ou mais saltos responde corretamente com modelo fake e grafo de teste, e uma busca vetorial
  isolada falha na mesma pergunta (o exemplo demonstra a diferença).
- Roteador: tabela de casos cobre as três rotas e o override manual.
- Nenhum import de terceiros fora de testes; exemplos rodam sem rede.

## 7. Deixado ao construtor

Nomes de campos e opções, formato exato dos prompts, estratégia de resolução de entidades duplicadas, estrutura interna do grafo em
memória, política de paralelismo na indexação, formato do `Trace`, e a divisão em commits.

## 8. Decisões do solicitante

1. **Métricas de cache**: sim. Adicionar campos de cache em `messages.Usage` (aditivo) e preenchê-los no provider Anthropic.
2. **Contador de tokens**: heurístico (~caracteres/4) atrás de interface substituível.
3. **Bancos de grafo**: Neo4j e Memgraph como únicos da primeira entrega.
4. **CLI**: incluir `cmd/knowledge`.
5. **Ordem de entrega**: núcleo e exemplos primeiro (`knowledge`, `rag`, `cag`, `kag`, grafo em memória, roteador, exemplos,
   manual); integrações depois (`Toolkit`, MCP, `integrations/graphstore`, CLI).

## 9. Próximo passo

Plano de implementação (`superpowers:writing-plans`), seguindo a ordem da decisão 5. A checagem de construtor por subagente não foi
rodada; o texto foi relido pelo próprio autor.
