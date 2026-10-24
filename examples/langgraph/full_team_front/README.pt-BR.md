# full_team_front

[English](README.md) | **Português** | [Español](README.es-ES.md)

Uma cópia de [full_team](../full_team/README.pt-BR.md) em que o time inteiro trabalha com front-end e experiência do usuário. Ele roda como servidor MCP: o VS Code (ou outro chat com MCP) envia uma demanda, e o time a transforma em código de telas revisado e em testes. A demanda passa pelo product owner, pelo time de engenharia, pelo desenvolvedor, pela revisão conjunta (especialista e tech lead) e pelo QA. Qualquer problema encontrado na revisão ou no QA volta para o desenvolvedor que escreveu o código.

Offline, roda com modelos falsos e um backend falso, então o fluxo inteiro funciona sem chave. Esta pasta é autocontida: o código do time foi copiado, não importado, porque o Go não permite importar o pacote `internal/` de outra árvore.

## O que faz

- **Time e modelos.** Doze workers, um processo cada quando executado como serviços. Cada papel tem um modelo; os padrões valem quando só `ANTHROPIC_API_KEY` está definida.

  | Worker | Quantidade | Modelo | Função |
  |---|---|---|---|
  | `product-owner` (`po`) | 1 | `claude-opus-5-5` | lê o backend, escreve a especificação |
  | `leader` (tech lead) e `manager` | 1 cada | `claude-sonnet-5-5` | planeja as tarefas, atribui, participa da revisão |
  | `software-engineer-junior` | 2 | `claude-haiku-5-5` | escreve o código das telas e os testes unitários |
  | `software-engineer-mid` | 2 | `claude-haiku-5-5` | o mesmo |
  | `software-engineer-senior` | 2 | `claude-haiku-5-5` | o mesmo |
  | `specialist-reviewer` | 2 | `claude-sonnet-5-5` | revisa o código junto com o tech lead |
  | `qa-engineer` | 2 | `claude-sonnet-5-5` | escreve os testes automatizados das telas |

  Substitua o modelo de um papel com `AGENT_MODEL_<ROLE>`, por exemplo `AGENT_MODEL_PO=anthropic:opus_5_5` ou `AGENT_MODEL_QA=openai:gpt-4o-mini`. `AGENT_MODEL` define um modelo para todos os papéis. Sem nenhuma chave, a execução é offline.

- **Fluxo.** O PO escreve a especificação a partir da demanda e do backend que lê. O leader planeja as tarefas e indica um desenvolvedor preferido para cada uma; a atribuição leva em conta a carga atual de cada worker. O desenvolvedor escreve o código das telas e os testes unitários. Em seguida roda a **revisão conjunta**: o especialista e o leader veem a mesma entrega, e a tarefa só passa quando os dois aprovam. Depois o QA escreve os testes automatizados das telas, rastreados à especificação do PO, e aprova quando eles a cobrem.

- **Retrabalho volta para o mesmo desenvolvedor.** Quando a revisão conjunta ou o QA rejeita uma tarefa, o feedback vai para o desenvolvedor que a escreveu, na mesma thread. Esse job de retrabalho é fixado: espera por aquele desenvolvedor em vez de ir para outro. Se o lease do desenvolvedor expirou, o job cai para um desenvolvedor livre do mesmo nível. Uma tarefa rejeitada em `MaxRounds` (3) rodadas termina `blocked`.

- **O PO lê o backend nomeado na demanda.** O PO tem três ferramentas somente leitura, e só ele as tem:
  - `backend_read`: lê um arquivo. O caminho deve estar sob uma raiz nomeada na demanda e sob `WORKSPACE_ROOT`. `..`, caminhos absolutos fora das raízes e symlinks que saem das raízes são recusados. O tamanho é limitado.
  - `backend_grep`: busca sob as mesmas raízes.
  - `backend_get`: somente HTTP `GET`; não há argumento de método. O host deve ser o host de uma URL nomeada na demanda. Só http e https. Redirecionamento para outro host é recusado. Destinos loopback e link-local são recusados, a menos que a demanda os nomeie. O corpo é limitado e a requisição tem timeout.

  As raízes e as URLs vêm apenas dos campos estruturados de `team_submit` (`backend_roots`, `backend_urls`), nunca de texto livre na requisição. `WORKSPACE_ROOT` é a raiz do workspace; por padrão é o diretório de trabalho. Uma demanda sem raízes e sem URLs não dá nenhum acesso ao PO.

- **A especificação.** O PO lista os endpoints que leu, os payloads de requisição e resposta, os campos obrigatórios, os erros e os estados da interface (carregando, vazio, erro). Todo payload descrito deve vir de algo que ele leu. A especificação tem um bloco `EVIDENCE:` (arquivo e linha, ou URL, para cada payload) e um bloco `OPEN ITEMS:` para o que ele não pôde confirmar. O PO não bloqueia por itens em aberto e não inventa payload. Se a demanda nomeia um backend e o PO não chamou nenhuma ferramenta de backend, ou se a especificação não tem bloco `EVIDENCE:`, o manager acrescenta `UNVERIFIED: no backend evidence` aos itens em aberto.

- **O QA escreve os testes.** O QA escreve um conjunto de testes por tarefa, para as telas que os desenvolvedores construíram, a partir da especificação do PO. Cada teste recebe o nome do critério ou do estado de interface que cobre, e uma linha `TRACE:` liga testes a critérios. O QA rejeita para o desenvolvedor quando o código da tela não consegue atender a um item da especificação (um estado errado, um campo ausente, um seletor ou um problema de acessibilidade).

- **Execução opcional no navegador (`browser_test`).** O QA pode rodar a sua especificação Playwright no Chrome. O comando é fixado pelo operador em `PLAYWRIGHT_CMD` (por exemplo `npx playwright test --project=chromium`) e roda sem shell. Ele roda contra o `app_url` nomeado na demanda. Se qualquer um dos dois faltar, a ferramenta informa `NOT RUN` com o motivo, e o QA continua escrevendo os testes. Offline devolve um relatório roteirizado.

- **Skills por persona.** Cada papel tem sua própria persona (front-end e UX) e skills de referência vendorizadas de repositórios públicos, em `skills/<role>/references/<name>/SKILL.md`. Elas são embutidas com `go:embed`, então o exemplo roda em qualquer máquina. Cada skill vendorizada está listada em `skills/skills.lock` com a origem, a licença, o SHA-256 do arquivo embutido e se foi reduzida para caber no orçamento de tamanho. Uma skill cuja licença não permite redistribuição é descartada, não parafraseada. As skills são fixadas: nada as atualiza automaticamente.

- **Compressão.** Todas as chamadas de modelo de cada worker passam pela mesma camada de compressão do `full_team` (caveman nos turnos antigos, Ponytail, TOON, RTK para comandos de shell). Ela não é descrita de novo aqui: veja [full_team](../full_team/README.pt-BR.md#o-que-faz).

- **Offline.** O PO roteirizado chama `backend_read`, `backend_grep` e `backend_get` contra um backend falso embutido: um pequeno documento OpenAPI, uma árvore de handlers e um servidor `httptest`. Assim as guardas rodam de verdade. O QA roteirizado escreve seus testes a partir da especificação offline.

## Como aplicar

- **Escopo da demanda.** Passe o backend na demanda, não no texto da requisição: `backend_roots` para árvores de código-fonte, `backend_urls` para a API que o PO pode chamar, `app_url` para o front-end em execução que o QA pode abrir.
- **Papéis e modelos.** `anthropicDefaults` e `Cast` em `internal/team` guardam os padrões. Altere-os ali e mantenha o pool de doze workers; os testes verificam esse número.
- **Ferramentas de backend.** Os limites (tamanho e timeout) estão na configuração das ferramentas de backend. Mantenha `WORKSPACE_ROOT` tão restrito quanto o trabalho permitir.
- **Skills.** Para mudar uma persona, edite o `SKILL.md` dela. Para adicionar uma skill vendorizada, veja abaixo.
- **Shell.** A ferramenta de shell é copiada do `full_team`. Com chave de provedor, o modelo só pode rodar `ls`, `grep` e `echo`. Não adicione interpretadores nem `find`.

### Atualizar uma skill vendorizada

1. Busque o candidato em um diretório temporário, não na pasta global de skills: `npx skills add <owner/repo@skill> -y` executado dentro de um projeto temporário. Não use `-g`.
2. Leia o `SKILL.md` e a licença (o arquivo ou a licença do repositório). Se a licença não permitir redistribuição, descarte a skill.
3. Copie o arquivo para `skills/<role>/references/<name>/SKILL.md`. Se estiver acima do orçamento (cerca de 8 KB por skill, 24 KB por papel), reduza-o e marque `trimmed` como `true` na entrada de `skills/skills.lock`.
4. Atualize a entrada em `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (SHA-256 do novo arquivo), `license`, `trimmed`.
5. Rode os testes abaixo. Eles verificam que toda entrada do lock tem o arquivo embutido, que o hash confere e os orçamentos de tamanho.

## Como executar

Offline, um processo:

```bash
go run ./examples/langgraph/full_team_front
```

A execução mostra os serviços que iniciou e o relatório de entrega. Ela inicia o hub, o manager e os workers em loopback e os encerra ao final.

Com um provedor real (os modelos são os padrões acima):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini para todos os papéis
```

## Usar como servidor MCP no VS Code

O time é servido por `Bridge.MCPServer` em `internal/team/bridge.go`, por stdio. Ele expõe estas ferramentas:

- `team_submit`: envia uma demanda. Os campos são `request` e, opcionalmente, `backend_roots`, `backend_urls` e `app_url`. Devolve `thread_id` e `run_id` de imediato.
- `team_status`: os status das tarefas, o relatório e o histórico de passagens de um `thread_id`.
- `team_queue`: os workers registrados e os jobs na fila.
- `team_ask_worker`: faz uma pergunta a um worker, fora do fluxo de entrega.
- `team_memory`: recupera o que o time registrou para uma requisição.
- As ferramentas genéricas de graph: `graph_run`, `graph_state` e `graph_resume`, para qualquer graph em um Agent Server.

A bridge conversa com um time que já está rodando: `MANAGER_URL` é a API Agent Server do manager e `HUB_URL` é o hub. O `go run` de um processo acima inicia os serviços só pelo tempo da execução, então ele não pode ser o servidor ao qual o VS Code se conecta.

Registre a bridge em `.vscode/mcp.json` (o formato do VS Code usa `servers`):

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "/abs/path/to/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000"
      }
    }
  }
}
```

Depois abra o arquivo e clique em **Start** acima do servidor. No Copilot Chat escolha **Agent** e habilite `front-team` no seletor de ferramentas.

Envie uma demanda com os campos de escopo. Este é um exemplo de chamada `team_submit` (os argumentos em JSON; os caminhos e URLs são exemplos):

```json
{
  "request": "Adicione um campo de cupom na tela de checkout. Mostre um estado de carregamento enquanto o cupom é validado, um estado vazio antes de qualquer digitação e uma mensagem de erro para um código inválido.",
  "backend_roots": ["/abs/path/to/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Depois chame `team_status` com o `thread_id` devolvido até o status ser `completed` ou `blocked`. As variáveis `PLAYWRIGHT_CMD` e `WORKSPACE_ROOT` são lidas pelos processos dos workers, onde as ferramentas rodam, não pela bridge.

## Como testar

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
```

Os testes cobrem o pool e os modelos, o retrabalho fixado, a revisão conjunta e o caminho de rejeição de volta ao mesmo desenvolvedor, o escopo de uma demanda, as guardas do backend do PO, a regra de evidência, os testes do QA e as recusas do `browser_test`, os arquivos de persona e skill, e a execução offline de ponta a ponta.

## Como testar em modo de produção

1. Rode com `ANTHROPIC_API_KEY` (ou `OPENAI_API_KEY`) e confira se os modelos do relatório são os que você definiu. `AGENT_MODEL_<ROLE>` substitui os padrões.
2. Envie uma demanda com `backend_roots` ou `backend_urls`. Confira se a especificação tem um bloco `EVIDENCE:` e se cada endpoint dele corresponde ao backend apontado. Uma especificação com `UNVERIFIED` nos itens em aberto significa que o PO não leu o backend.
3. Experimente uma demanda que nomeia um host que o PO não deveria alcançar, e uma URL que redireciona para outro host. As duas devem ser recusadas na saída da ferramenta.
4. Rode o QA com `PLAYWRIGHT_CMD` e `app_url` definidos no host dos workers, e confira se o relatório mostra o resultado do Playwright, não `NOT RUN`. Confirme que o Chrome está instalado onde os workers rodam.
5. Mantenha `WORKSPACE_ROOT` e as raízes do backend tão restritas quanto o trabalho permitir. As ferramentas de backend leem arquivos no host que roda o worker do PO.
6. Mantenha `AllowedCommands` restrito: a ferramenta de shell roda no host, sem sandbox.

## Veja também

- [full_team](../full_team/README.pt-BR.md): o time copiado, com a camada de compressão documentada por completo
- [independent_engineering_team](../independent_engineering_team/README.pt-BR.md): o mesmo time dividido em serviços, com Docker compose
- [compress_shell_rtk](../compress_shell_rtk/README.pt-BR.md), [compress_toon](../compress_toon/README.pt-BR.md), [compress_ponytail](../compress_ponytail/README.pt-BR.md)
