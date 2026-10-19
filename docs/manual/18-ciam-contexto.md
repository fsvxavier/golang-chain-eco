# 18. ciam: contexto de projeto barato e preciso para assistentes

Assistentes de IA gastam tokens relendo arquivos inteiros e ainda erram por não conhecerem o **porquê** do código. O pacote `ciam` resolve os dois lados: um **índice local com busca híbrida** (devolve só os trechos relevantes) e uma pasta `docs/` com **ADR, PRD e planos** versionados e pesquisáveis. Referência e limites: [docs/CIAM.md](../CIAM.md).

Pré-requisitos: capítulo 6 (embeddings e RAG) e, para a parte de agents, capítulos 5 e 9.

## 18.1 O fluxo em uma tela

```bash
go install ./cmd/ciam
cd meu-projeto
ciam adr new "Usar JWT para sessões"     # 1. escreva o PORQUÊ antes do código
ciam index                               # 2. indexe (incremental)
ciam search "autenticação" --type view   # 3. busque trechos, não arquivos
ciam decision "adicionar refresh token"  # 4. código + ADR + PRD + planos numa chamada
ciam remember "tokens expiram em 15 min" --type decision
ciam init --editor claude                # 5. conecte o editor (MCP)
```

Sem Ollama rodando, a CLI usa embeddings por hashing e avisa; para busca semântica de verdade: `ollama pull nomic-embed-text` (100% local).

## 18.2 Indexar e buscar na API

```go
emb := &ollama.Embeddings{Model: "nomic-embed-text"}
fs, _ := ciam.OpenFileStore("ciam.json")        // ou qualquer store.Store (sqlstore, redis, mongo)
m := ciam.New(emb, fs)

res, _ := m.Index(ctx, "/projeto", ciam.IndexOptions{})
fmt.Println(res.Changed, "arquivos reprocessados,", res.Unchanged, "intactos")

hits, _ := m.Search(ctx, ciam.SearchRequest{
	Project: "projeto", Query: "validação do token JWT",
	Types: []string{"view", "handler"}, Limit: 5, Compress: true,
})
```

- **Incremental**: só arquivos com hash novo são cortados e embeddados; removidos saem do índice. Reindexar custa quase nada (`ciam watch` faz isso a cada 30 s).
- **Híbrida**: `Result.Vector` (cosseno) e `Result.BM25` mostram cada metade; a nota final é a fusão por *reciprocal rank*. Identificadores `snake_case`/`camelCase` são partidos: "stripe customer" acha `stripe_customer_id`.
- **Tipos de chunk**: `model`, `view`, `url`, `serializer`, `task`, `test`, `handler`, `config`, `doc`, `schema`, `generic` e os documentos `adr`, `prd`, `plan`, `research`. Para outros padrões, passe `Manager.Classifiers`.
- **Compressão**: `Compress: true` remove docstrings/comentários; economiza tokens na resposta.

`m.Status(ctx)` mostra tokens servidos e a **estimativa** do que ler os arquivos inteiros teria custado a mais.

## 18.3 ADR, PRD e planos

```go
docs := ciam.NewDocs("/projeto")
docs.New(ciam.PRD, "Billing via Stripe", "")
docs.New(ciam.Plan, "Integração Stripe", "PRD-001")   // vincula o plano ao PRD
docs.New(ciam.ADR, "Corrigir race no worker de e-mails", "")  // causa-raiz ANTES do fix
docs.SupersedeADR(3, "Usar ULID como chave primária")  // ADR-003 vira "superseded"; nada é reescrito
list, _ := docs.List(ciam.ADR)                         // número, título, status
```

Arquivos em `docs/adr|prd|plans|research/PREFIXO-NNN-slug.md` com *front matter* (`status`, `date`, `title`). Depois de escrever, `ciam index`: viram chunks pesquisáveis. Regra de ouro: **feature nova → PRD + plano; bug relevante → ADR com a causa-raiz; mudou de ideia → supersede, não edite**.

`m.DecisionContext(ctx, projeto, "o que vou fazer", 3)` devolve em Markdown: código (comprimido), ADRs, PRDs, planos e pesquisas relevantes — é o que um agent deve consultar **antes de implementar**.

## 18.4 Memória entre sessões

`Remember`/`Recall` guardam decisões e notas que valem para além de um projeto ("deploy só às terças"). São memórias do `langmem` (cap. 16) no namespace `["ciam","memories"]`: um `langmem.Manager` ou a tool `manage_memory` podem curá-las também.

## 18.5 Dar o contexto a um agent

```go
kit := ciam.Toolkit{M: m, Project: "/projeto"}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

Tools (`ciam_*`): `route`, `index`, `search`, `context` (busca + compressão), `decision_context`, `project_map`, `adr_search`, `prd_search`, `plan_search`, `research_search`, `remember`, `recall`, `compress`, `doc_new` e, se `Manager.Model` existir, `draft`. `ciam.Instructions` ensina a ordem: **decision_context antes de implementar; search antes de ler arquivos; registre decisões**. Exemplo: `go run ./examples/ciam/agent_with_context` — o agent descobre, pelo ADR, que dinheiro é `int64` em centavos, algo que o pedido não dizia.

No engenheiro do Open SWE (cap. 19): `openswe.Config{Context: &kit}`.

## 18.6 Servir para o editor (MCP)

```bash
ciam init --editor vscode     # ou cursor / claude; mescla com servidores já configurados
```

Isso grava `{"command": ".../ciam", "args": ["mcp"], "env": {"CIAM_PROJECT_PATH": ...}}`. Em código: `kit.MCPServer(onCall).ServeStdio(ctx, os.Stdin, os.Stdout)` ou `.Handler()` para HTTP (`ciam serve`). O servidor também publica *prompts* (`implement-feature`, `investigate-bug`) e as instruções acima. O pacote `mcp/server` é genérico: serve qualquer `[]tools.Tool` seu.

## 18.7 Rascunhos com modelo local (opcional)

`Manager.Model = ollama.New("qwen2.5-coder:1.5b")` habilita `ciam_draft`: um rascunho de código grounded no contexto e numa fase do plano (`PlanID`, `Phase: "Phase 1"`), marcado como **especulativo**. Use para poupar tokens pagos em boilerplate; valide contra o plano e os ADRs.

## 18.8 Cuidados

- **Segredos**: o índice contém o código. Mantenha `.env`, chaves e dumps fora do projeto indexado (ou fora das extensões indexáveis) e guarde o arquivo do índice com as mesmas permissões do código.
- **Escala**: a busca carrega o corpus do projeto em RAM e varre por cosseno. Funciona bem para projetos de tamanho normal; para monorepos enormes, indexe por subprojeto (`IndexOptions.Project`).
- **Equipe**: `FileStore` é de um usuário. Para time use um store compartilhado, e versione `docs/` no git (é a parte que realmente vale para todos).

Exemplos: `go run ./examples/ciam/project_context` e `.../agent_with_context`.

---

Anterior: [17. Agent Protocol](17-agent-protocol.md) · Próximo: [19. Open SWE](19-open-swe.md) · [Índice do manual](README.md)
