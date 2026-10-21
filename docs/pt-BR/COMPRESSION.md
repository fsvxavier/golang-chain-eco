# Compressão de chamadas: RTK, Caveman e um pequeno modelo de decisão

[English](../en-US/COMPRESSION.md) | **Português** | [Español](../es-ES/COMPRESSION.md)

Referência do pacote `compress`, do middleware `Compression` em `agents/middleware` e do hook `backends.LocalShell.Rewrite`.
O código da biblioteca usa só a biblioteca padrão. O decider nativo de Laya é um módulo Go separado.

## A ideia

Agentes gastam a maior parte dos tokens em duas coisas: históricos longos de conversa e saída barulhenta de shell. A compressão é decidida por chamada, a partir de um resumo pequeno, por um decider:

- **Caveman** encurta o texto dos turnos antigos e pede uma resposta concisa.
- **RTK** (a CLI `rtk`, opcional) encurta a saída de comandos de shell simples.
- **Um decider** responde perguntas tipadas sobre o estado atual. Pode ser um conjunto de regras (`RuleDecider`) ou um modelo System 1: Laya (`HTTPDecider` ou o módulo nativo) ou, depois, TypeSafe JEV.

Um modelo nunca lê o histórico inteiro. Ele lê um `Summary` (contagens, tamanhos, parcela de saída de ferramentas, idioma, uma cauda curta) que cabe na janela dele. A compressão nunca reescreve o estado guardado nem os checkpoints: ela muda só a cópia por chamada enviada ao modelo, então uma thread retomada vê o texto original.

## As quatro perguntas

| Id | Onde | Tipo | Efeito com "sim" (ou o nível escolhido) |
|---|---|---|---|
| `compress_history` | antes de uma chamada ao modelo | Noul | comprime turnos antigos com Caveman |
| `level` | antes de uma chamada ao modelo (perguntada quando histórico ou resposta estão ligados) | Choice: `lite`, `full`, `ultra` | nível do Caveman, compartilhado por histórico e resposta |
| `compress_reply` | antes de uma chamada ao modelo | Noul | acrescenta a diretiva de resposta concisa após a última mensagem do usuário |
| `route_rtk` | antes de um comando de shell | Noul | prefixa o comando com `rtk ` |

Os ids de pergunta e resposta são strings exatas (`compress.QCompressHistory`, `QLevel`, `QCompressReply`, `QRouteRTK`). As respostas são associadas pelo id. Respostas Noul trazem um valor booleano e uma probabilidade; respostas Choice trazem uma escolha e uma distribuição.

## Orçamento do Summary

`compress.Summarize(messages, SummaryOptions{})` monta um `Summary`: contagem de mensagens e turnos, tokens estimados, parcela de saída de ferramentas, tamanho do maior resultado de ferramenta, idioma dominante (inglês, português, espanhol ou outro, pela contagem de stopwords) e as últimas mensagens, truncadas. Tokens são estimados como bytes / 4.

O estado codificado (`Summary.State()` em JSON) é cortado em um orçamento de **700 tokens**, para que as perguntas caibam em uma janela de 1024 tokens do decider. Uma mensagem enorme, texto com unicode e histórico vazio ficam todos abaixo do orçamento (testado).

## Decider e fallback

`compress.Decider` tem um método: `Decide(ctx, State, []Question) ([]Answer, error)`.

`RuleDecider` não precisa de modelo. Os padrões:

- `compress_history`: sim quando o histórico passa de **4000** tokens estimados.
- `level`: `full`.
- `route_rtk`: sim quando a primeira palavra é uma de `git go ls grep find docker kubectl npm cargo pytest` e a saída anterior desse comando passou de **2048** bytes ou é desconhecida.

`compress.NewGuarded(inner)` envolve um decider com estes padrões: fallback `RuleDecider{}`, **piso de confiança 0.7**, **timeout do decider 2s**. Pergunta por pergunta, a resposta do interno só é usada quando existe, tem o id e o tipo certos e é confiável. A confiança é a probabilidade da classe mais provável em Choice e `max(P, 1-P)` em Noul. Qualquer outra resposta, erro do interno, timeout ou panic envia aquela pergunta ao fallback. O cancelamento do contexto de quem chama é devolvido como erro. Um decider nunca derruba uma execução.

As probabilidades de um modelo não são calibradas. O piso é uma configuração a ajustar com dados reais.

## Caveman (compressão de texto)

`compress.Compress(text, lvl)` trabalha na prosa dos turnos antigos. Trechos protegidos saem byte a byte iguais: blocos de código cercados, código inline, URLs, caminhos, strings entre aspas, linhas parecidas com JSON e código indentado.

- **Regras mecânicas, qualquer idioma, todo nível:** normaliza CRLF para LF, colapsa sequências de espaços e linhas em branco, remove espaços no fim das linhas, remove linhas duplicadas consecutivas, colapsa `!` e `?` repetidos.
- **Tabelas de palavras** (`Lexicon`) para `en`, `pt`, `es`, `fr`, `de` e `it`: `lite` remove também enchimentos; `full` remove também artigos e hesitações; `ultra` remove também conectivos iniciais de frase.
- **Detecção de idioma** por chamada, contando stopwords nas tabelas registradas. Sem vencedor claro, ou texto com menos de 5 palavras, valem só as regras mecânicas. Idioma sem tabela (por exemplo japonês ou árabe) recebe só compressão de espaços e repetições.
- **`compress.RegisterLexicon(l)`** adiciona ou substitui uma tabela (seguro para uso concorrente).
- A saída nunca é mais longa que a entrada, nunca fica vazia, e a compressão é idempotente.

`compress.ReplyDirective` é a nota de uma linha para resposta concisa: `Reply tersely: no filler, no pleasantries, keep code and numbers exact.`

O Caveman nunca toca em resultados de ferramentas, argumentos de chamadas de ferramenta, prompts de sistema, JSON, código, caminhos, URLs, números ou erros entre aspas.

## RTK (saída de shell)

`compress.NewRTK(decider)` decide antes de executar, a partir do texto do comando e do tamanho da saída anterior do mesmo comando. Nunca executa um comando duas vezes para medir.

- Só comandos **simples** são candidatos. Qualquer comando com pipe, `&&`, `||`, `;`, redirecionamento, `$(...)`, crases, quebra de linha ou `VAR=` no início fica de fora.
- O `rtk` é procurado com o `LookPath` sensível ao ambiente. `rtk` ausente significa passar direto.
- O `AllowedCommands` do `LocalShell` é verificado no comando **original**, antes de qualquer reescrita.
- `LocalShell.Rewrite` é aplicado depois dessa verificação, ao comando que é executado. Nil mantém o comportamento antigo.
- O RTK preserva o código de saída, e as linhas de erro de `go test` e `go build` sobrevivem (testado).

## Middleware de compressão

`middleware.NewCompression(decider)` (em `agents/middleware`) é uma transformação `WrapModelCall`:

- Monta o `Summary`, pergunta ao decider com guarda, e aplica Caveman aos turnos antigos de Human e AI, e a diretiva de resposta após a última mensagem Human.
- `Keep` é o número de turnos recentes que não são tocados (padrão **4**). `Floor` é o piso de confiança (padrão **0.7**).
- Chamadas e resultados de ferramentas nunca são tocados, então cada id de chamada e a ordem dos pares sobrevivem.
- Só a cópia por chamada muda. O estado guardado e os checkpoints mantêm o texto original.
- A diretiva de resposta fica após a última mensagem do usuário, não no prompt de sistema, para que prefixos estáveis em bytes (`cag`) mantenham o cache.
- `Summarization` continua sendo um middleware `BeforeModel` separado, não uma etapa dentro de Compression (a ordem dos hooks torna impossível "só se ainda passar do limite depois do Caveman"). Defina o `MaxTokens` dele acima do limiar do decider para que atue como **retaguarda** em históricos que o Caveman não encolhe o suficiente.

## HTTPDecider: servidor Laya

`compress.HTTPDecider{URL, APIKey, Model, Client}` consulta um servidor Laya por HTTP com `net/http`, sem código de terceiros. URL padrão: `http://localhost:8000/v1/systemone`.

O servidor oficial é o `laya-serve`, um pacote Python:

```bash
pip install "laya[serve]"
LAYA_MODELS=... LAYA_DEVICE=cpu LAYA_HOST=127.0.0.1 LAYA_PORT=8000 LAYA_API_KEY=... laya-serve
```

Variáveis de ambiente: `LAYA_MODELS` (modelos a carregar), `LAYA_DEVICE`, `LAYA_HOST`, `LAYA_PORT`, `LAYA_API_KEY` (enviada como chave bearer). A forma `laya serve --model` não aparece na documentação pública e não é usada aqui.

O contrato é `POST /v1/systemone`: um corpo JSON com o estado e as perguntas tipadas, e uma resposta JSON com as respostas tipadas. É o mesmo protocolo do TypeSafe JEV, então um endpoint JEV deve funcionar com URL e chave. **Isso não foi testado.**

**Ponto em aberto:** o formato da resposta Noul do `laya-serve` (booleano com ou sem probabilidade) não está documentado aqui. Um booleano sem confiança cai nas regras pela guarda. Verifique uma resposta real antes de depender das probabilidades da Laya.

O exemplo `decider_laya_http` roda offline contra um servidor falso em processo. Com `LAYA_URL` (e `LAYA_API_KEY`, `LAYA_MODEL`) usa um servidor real.

## Laya nativo (laya-go)

`github.com/metalagman/laya-go` v0.1.0 roda o modelo em processo. Precisa de cgo e ONNX Runtime, então vive em **módulo Go próprio**, não no módulo principal, e é compilado com a build tag `laya_native`. Qualificação: **linux/amd64, somente FP32**. Entrada maior que a janela do modelo devolve `ErrInputTooLong`, a menos que o truncamento esteja ligado.

Exemplo: `examples/langgraph/decider_laya_native` (com `go.mod` próprio e `replace` para a raiz do repositório). Sem a tag ou sem `LAYA_BUNDLE_DIR`, ele informa o motivo e usa `RuleDecider`. O módulo principal nunca importa o laya-go, e `go build ./...` não precisa de cgo.

## JEV (planejado)

TypeSafe JEV é o segundo modelo System 1. Está citado aqui como planejado e **não construído**: não há pasta `decider_jev` nem código. A página pública do JEV não declara API, suporte em Go nem tamanho de janela. O cliente HTTP acima é o caminho a tentar primeiro.

## Exemplos

Todos rodam offline com um modelo falso, a menos que um serviço real esteja configurado. Cada um tem `README.md`, `README.pt-BR.md` e `README.es-ES.md`.

| Exemplo | Mostra |
|---|---|
| `langgraph/decider_fallback` | a interface `Decider`, `RuleDecider` e `Guarded`, sem modelo |
| `langgraph/compress_history_summary` | `Summary` de um histórico acima de 8k tokens, cabe no orçamento de 700 tokens |
| `langgraph/compress_shell_rtk` | saída de shell pelo RTK quando o decider decide |
| `langgraph/compress_prompt_caveman` | compressão de prompt e de resposta com o middleware |
| `langgraph/decider_laya_http` | `HTTPDecider` contra o `laya-serve` (servidor falso offline) |
| `langgraph/decider_laya_native` | laya-go nativo, módulo separado, tag `laya_native` |
| `langgraph/full_team` | o time de `independent_engineering_team` com compressão de chamadas; decider e modelo falsos por padrão |

Teste em modo de produção: configure a URL real do decider ou o bundle nativo, e rode `go test -race -count=1` no módulo do exemplo.
