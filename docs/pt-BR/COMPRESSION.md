# Compressão de chamadas: RTK, Caveman, Ponytail, TOON e um pequeno modelo de decisão

[English](../en-US/COMPRESSION.md) | **Português** | [Español](../es-ES/COMPRESSION.md)

Referência do pacote `compress`, do middleware `Compression` em `agents/middleware` e do hook `backends.LocalShell.Rewrite`.
O código da biblioteca usa só a biblioteca padrão. O `go.mod` da raiz exige o laya-go para o decider nativo de Laya, mas só a build tag `laya_native` o compila. `examples/langgraph/decider_laya_native` é um módulo Go separado.

## A ideia

Agentes gastam a maior parte dos tokens em duas coisas: históricos longos de conversa e saída barulhenta de shell. A compressão é decidida por chamada, a partir de um resumo pequeno, por um decider:

- **Caveman** encurta o texto dos turnos antigos e pede uma resposta concisa.
- **Ponytail** é uma diretiva de prompt que faz um agente de código escrever menos código. É um texto de regras, não um compressor.
- **TOON** recodifica resultados JSON de ferramentas em uma notação mais compacta, só quando o resultado fica estritamente menor.
- **RTK** (a CLI `rtk`, opcional) encurta a saída de comandos de shell simples.
- **Um decider** responde perguntas tipadas sobre o estado atual. Pode ser um conjunto de regras (`RuleDecider`) ou um modelo System 1: Laya (`HTTPDecider` ou o módulo nativo) ou, depois, TypeSafe JEV.

Um modelo nunca lê o histórico inteiro. Ele lê um `Summary` (contagens, tamanhos, parcela de saída de ferramentas, idioma, uma cauda curta) que cabe na janela dele. A compressão nunca reescreve o estado guardado nem os checkpoints: ela muda só a cópia por chamada enviada ao modelo, então uma thread retomada vê o texto original.

## As perguntas

| Id | Onde | Tipo | Efeito com "sim" (ou o nível escolhido) |
|---|---|---|---|
| `compress_history` | antes de uma chamada ao modelo | Noul | comprime turnos antigos com Caveman |
| `level` | antes de uma chamada ao modelo (sempre perguntada, no mesmo lote de chamadas do decider; aplicada só quando histórico, resposta ou Ponytail estão ligados) | Choice: `lite`, `full`, `ultra` | nível do Caveman e da diretiva Ponytail, compartilhado por histórico e resposta |
| `compress_reply` | antes de uma chamada ao modelo | Noul | acrescenta a diretiva de resposta concisa após a última mensagem do usuário |
| `ponytail` | antes de uma chamada ao modelo | Noul | acrescenta a diretiva Ponytail após a última mensagem do usuário |
| `toon_tool_results` | antes de uma chamada ao modelo | Noul | reescreve resultados JSON de ferramentas em TOON quando estritamente menores |
| `route_rtk` | antes de um comando de shell | Noul | prefixa o comando com `rtk ` |

As cinco primeiras formam o conjunto de chamadas ao modelo (`compress.HistoryQuestions()`, nessa ordem). `route_rtk` é perguntada antes de um comando de shell, não antes de uma chamada ao modelo.

Os ids de pergunta e resposta são strings exatas (`compress.QCompressHistory`, `QLevel`, `QCompressReply`, `QPonytail`, `QTOON`, `QRouteRTK`). As respostas são associadas pelo id. Respostas Noul trazem um valor booleano e uma probabilidade; respostas Choice trazem uma escolha e uma distribuição.

## Orçamento do Summary

`compress.Summarize(messages, SummaryOptions{})` monta um `Summary`: contagem de mensagens e turnos, tokens estimados, parcela de saída de ferramentas, tamanho do maior resultado de ferramenta, idioma dominante (inglês, português, espanhol ou outro, pela contagem de stopwords) e as últimas mensagens, truncadas. Dois sinais alimentam as perguntas Ponytail e TOON: `CodeShare` (a parcela de bytes dentro de blocos de código cercados nas mensagens Human e AI, de 0 a 1; chave de estado `code_share`) e `JSONToolBytes` (o total de bytes das mensagens de ferramenta cujo conteúdo é um objeto ou array JSON; chave de estado `json_tool_bytes`). Tokens são estimados como bytes / 4.

Com o `SummaryOptions` padrão, o estado codificado (`Summary.State()` em JSON) fica abaixo de **700 tokens**, para que as perguntas caibam em uma janela de 1024 tokens do decider. Uma mensagem enorme, texto com unicode e histórico vazio ficam todos abaixo do orçamento (testado). `SummaryOptions` maiores não são cortados, então podem ultrapassá-lo.

## Decider e fallback

`compress.Decider` tem um método: `Decide(ctx, State, []Question) ([]Answer, error)`.

`RuleDecider` não precisa de modelo. Os padrões:

- `compress_history`: sim quando o histórico passa de **4000** tokens estimados.
- `level`: `full`.
- `ponytail`: sim quando `code_share >= 0.1`.
- `toon_tool_results`: sim quando `json_tool_bytes >= 2048`.
- `route_rtk`: sim quando a primeira palavra é uma de `git go ls grep find docker kubectl npm cargo pytest` e a saída anterior desse comando passou de **2048** bytes ou é desconhecida.

`compress.NewGuarded(inner)` envolve um decider com estes padrões: fallback `RuleDecider{}`, **piso de confiança 0.7**, **timeout do decider 2s**. Pergunta por pergunta, a resposta do interno só é usada quando existe, tem o id e o tipo certos e é confiável. A confiança é a probabilidade da classe mais provável em Choice e `max(P, 1-P)` em Noul. Qualquer outra resposta, erro do interno, timeout ou panic envia aquela pergunta ao fallback. O cancelamento do contexto de quem chama é devolvido como erro. Um decider nunca derruba uma execução.

As probabilidades de um modelo não são calibradas. O piso é uma configuração a ajustar com dados reais.

## Caveman (compressão de texto)

`compress.Compress(text, lvl)` trabalha na prosa dos turnos antigos. Trechos protegidos saem byte a byte iguais: blocos de código cercados, código inline, URLs, caminhos, strings entre aspas, linhas parecidas com JSON e código indentado. Linhas inteiras também ficam intactas quando são linhas de tabela Markdown (primeiro caractere não espaço `|`) ou parecem código: contêm `==`, `!=`, `:=`, `=>`, `->`, `&&`, `||`, `{` ou `}`, ou terminam com `;` ou `) {`.

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
- O RTK preserva o código de saída, e as linhas de erro de `go vet` sobrevivem (testado).

## Ponytail (menos código escrito)

`compress.PonytailDirective(lvl)` devolve um texto de regras para um agente de código. A diretiva é adaptada do Ponytail (https://github.com/DietrichGebert/ponytail, licença MIT, de Dietrich Gebert); veja esse repositório para o texto da licença.

- **Níveis.** `Lite` são duas frases. `Full` (o padrão) é uma escada de sete degraus: precisa existir, o código já tem, a biblioteca padrão resolve, um recurso nativo da plataforma cobre, uma dependência instalada resolve, cabe em uma linha, e só então o código mínimo. `Ultra` é o conjunto mais longo e acrescenta "perguntar se precisa existir". Cada texto tem menos de 1200 bytes e termina com uma linha `Ponytail:`.
- **A regra que mantém a segurança.** O texto diz "preguiçoso com a solução, nunca com a leitura": o agente precisa ler o código afetado de ponta a ponta antes de escolher um degrau. Nunca manda o modelo pular validação de entrada em fronteiras de confiança, tratamento de erro que evita perda de dados, checagens de segurança, o básico de acessibilidade, ou o único teste que guarda uma lógica arriscada.
- **Onde entra.** `middleware.Compression` acrescenta `"\n\n" + PonytailDirective(level)` à última mensagem Human da cópia por chamada, uma vez por chamada, antes da diretiva de resposta quando ambas valem. O prompt de sistema não muda, então prefixos estáveis em bytes mantêm o cache.
- **Quando é perguntada.** A pergunta `ponytail` é sim quando `CodeShare >= 0.1`, então só conversas com uma parcela real de código recebem a nota.

O que se sabe e o que não se sabe:

- O fornecedor relata **código -53% e tokens -45%** em sua própria execução de 39 tarefas. Esses são os números de benchmark do próprio fornecedor, publicados em https://github.com/DietrichGebert/ponytail. Não são reproduzidos aqui, e este projeto não os mediu.
- A diretiva é um empurrão. Se um modelo escreve menos código depende do modelo e da tarefa. Meça com um modelo real (veja o exemplo abaixo); o exemplo offline mostra apenas o que o modelo recebe.

## TOON (resultados JSON de ferramentas)

TOON (Token-Oriented Object Notation) é uma codificação compacta, baseada em indentação, de JSON. Este pacote segue a **especificação v4.4** (`https://github.com/toon-format/spec`, `SPEC.md`) para o subconjunto que implementa.

- `compress.TOONFromJSON(text, compress.TOONOptions{Delimiter})` codifica texto JSON. O delimitador é `","` (padrão), `"\t"` ou `"|"`; qualquer outro valor é erro. JSON inválido é erro.
- `compress.TOONShorter(text)` devolve o texto TOON e `true` só quando `text` é um objeto ou array JSON **e** o TOON é estritamente menor. Caso contrário devolve a entrada e `false`. Use quando a saída nunca pode ser maior que a entrada.
- Implementado: indentação de 2 espaços, LF, sem quebra de linha final; chaves sem aspas só quando casam com `^[A-Za-z_][A-Za-z0-9_.]*$`; strings entre aspas quando vazias, parecidas com número, `true`/`false`/`null`, com espaços nas pontas, contendo `: " \ [ ] { }`, um caractere de controle ou o delimitador ativo, ou começando com `-` ou `#`; ordem das chaves mantida como no texto JSON; números em decimal simples com os dígitos mais curtos que fazem ida e volta (forma exponencial fora de `1e-6 <= |n| < 1e21`), `-0` como `0`, NaN e Inf como `null`, inteiros grandes mantidos dígito a dígito.
- Arrays: arrays de primitivos inline (`tags[3]: admin,ops,dev`); arrays de objetos com as mesmas chaves e só valores primitivos como bloco **tabular** (`users[2]{id,name,role}:` com uma linha por objeto); arrays mistos ou não uniformes em forma de lista, com itens `- `; arrays vazios como `key: []`. Um array cujas colunas são objetos aninhados (por exemplo `{"a":{"z":1}}`) usa a forma de lista, não o bloco tabular.
- **Não implementado:** não há **decodificador** (TOON de volta para JSON), nem a forma **tabular com chaves**, nem **key folding**. Objetos aninhados são escritos como indentação aninhada. A perda zero depende dos testes golden em `compress/toon_test.go`, que usam os exemplos da especificação, não um ciclo de ida e volta.

- **Chaves duplicadas** seguem o `encoding/json`: vale o último valor, na posição da primeira ocorrência, então `{"a":1,"b":2,"a":3}` vira `a: 3` e depois `b: 2`. Um decodificador TOON estrito rejeita chave irmã repetida, então nenhuma é escrita.
- **Números.** Inteiros acima de 2^53 são escritos com todos os dígitos, então um decodificador JavaScript pode arredondá-los. Números além do intervalo de float, como `1e400`, viram `null`.

**No middleware.** A pergunta `toon_tool_results` é sim quando `JSONToolBytes >= 2048`. Aí cada mensagem de ferramenta cujo conteúdo é um objeto ou array JSON, e que não tem `Parts`, é trocada na cópia por chamada pela forma TOON, mas só quando essa forma é estritamente menor. Os ids das chamadas de ferramenta e a ordem das mensagens nunca mudam. Saída de ferramenta que não é JSON e mensagens com `Parts` ficam intactas. O estado guardado não é reescrito.

Essa é a única exceção a "resultados de ferramentas nunca são tocados". O Caveman continua sem tocar neles.

**A economia depende do modelo.** O TOON só ajuda se o modelo o ler bem, e a economia de tokens do modelo varia com o tokenizador. O projeto TOON publica seus próprios benchmarks; eles não são reproduzidos aqui. O exemplo informa bytes e uma estimativa (`messages.ApproxTokens`, bytes / 4), não tokens cobrados.

## Middleware de compressão

`middleware.NewCompression(decider)` (em `agents/middleware`) é uma transformação `WrapModelCall`:

- Monta o `Summary`, pergunta ao decider com guarda, e aplica Caveman aos turnos antigos de Human e AI, a diretiva Ponytail (quando perguntada) e a diretiva de resposta após a última mensagem Human, e TOON aos resultados JSON de ferramentas (quando perguntado).
- `Keep` é o número de turnos recentes que não são tocados (padrão **4**). O piso de confiança (**0.7**) é definido em `compress.Guarded`, não em `Compression`: `NewCompression` envolve o decider com `NewGuarded`. Para mudá-lo, monte um `compress.Guarded{...}` e passe-o como `Compression{Decider: g}`.
- Chamadas de ferramentas nunca são tocadas. Resultados de ferramentas mudam só pela regra TOON acima, então cada id de chamada e a ordem dos pares sobrevivem.
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

**Privacidade:** a requisição leva o estado do `Summary`, incluindo a cauda: as últimas 3 mensagens, cada uma cortada em 160 bytes de texto codificado em JSON, com a saída das ferramentas. A cauda vai para a URL do decider, então use um endpoint confiável. O middleware sempre usa o `SummaryOptions` padrão, e `TailMessages` 0 significa o padrão 3, não nenhuma. Para manter a cauda na sua máquina, use um decider local (`RuleDecider`, ou o decider nativo laya-go), não o `HTTPDecider`.

O contrato é `POST /v1/systemone`: um corpo JSON com o estado e as perguntas tipadas, e uma resposta JSON com as respostas tipadas. É o mesmo protocolo do TypeSafe JEV, então um endpoint JEV deve funcionar com URL e chave. **Isso não foi testado.**

**Ponto em aberto:** o formato da resposta Noul do `laya-serve` (booleano com ou sem probabilidade) não está documentado aqui. Um booleano sem confiança cai nas regras pela guarda. Verifique uma resposta real antes de depender das probabilidades da Laya.

**Ids das perguntas e tamanho do lote.** Os ids das perguntas são as chaves do mapa `questions` da requisição, e as respostas voltam com as mesmas chaves. O código público do `laya-serve` (`laya/serve.py`, github.com/NandhaKishorM/laya) foi lido por uma busca na web, que devolve um resumo e não o arquivo bruto: ele não valida os ids (só a quantidade de perguntas, no máximo 64 por requisição) e não valida perguntas `noul`. Isso **não foi verificado contra um servidor rodando**; o `Router.predict` da biblioteca não foi lido. Este pacote envia 5 perguntas por chamada. Se um servidor rejeitar a requisição inteira, todas as perguntas caem na regra; se só omitir algumas respostas, só essas caem.

**Respostas omitidas.** Um servidor que deixa de fora a resposta de algumas perguntas faz só essas perguntas caírem nas regras, pelo `NewGuarded`. A chamada falha só quando nenhum dos ids pedidos é respondido, com status diferente de 200, ou com JSON inválido.

O exemplo `decider_laya_http` roda offline contra um servidor falso em processo. Com `LAYA_URL` (e `LAYA_API_KEY`, `LAYA_MODEL`) usa um servidor real.

## Laya nativo (laya-go)

`github.com/metalagman/laya-go` v0.2.6 roda o modelo em processo. Precisa de cgo e ONNX Runtime. O `go.mod` da raiz o exige, mas ele só é compilado com a build tag `laya_native`, em `examples/langgraph/full_team/native`, um pacote do módulo raiz. Qualificação: **linux/amd64, somente FP32**. Entrada maior que a janela do modelo devolve `ErrInputTooLong`, a menos que o truncamento esteja ligado.

Exemplo: `examples/langgraph/decider_laya_native` é um módulo Go separado (com `go.mod` próprio e `replace` para a raiz do repositório). Sem a tag ou sem `LAYA_BUNDLE_DIR`, ele informa o motivo e usa `RuleDecider`. Sem a tag, `go build ./...` não compila o laya-go e não precisa de cgo.

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
| `langgraph/compress_toon` | um resultado JSON de 20 linhas, sua forma TOON, tamanhos antes e depois, e o fallback `TOONShorter` para um objeto pequeno |
| `langgraph/compress_ponytail` | a nota Ponytail que o modelo recebe em cada nível, e a prova de que o prompt de sistema não muda |
| `langgraph/decider_laya_http` | `HTTPDecider` contra o `laya-serve` (servidor falso offline) |
| `langgraph/decider_laya_native` | laya-go nativo, módulo separado, tag `laya_native` |
| `langgraph/full_team` | o time de `independent_engineering_team` com compressão de chamadas; decider e modelo falsos por padrão. Com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY`, a ferramenta de shell oferece só `ls`, `grep` e `echo` |

Teste em modo de produção: configure a URL real do decider ou o bundle nativo, e rode `go test -race -count=1` no módulo do exemplo.
