# Usando o router com vários gateways

[English](../en-US/gateway-router.md) | **Português** | [Español](../es-ES/gateway-router.md)

`router:<model>` envia cada chamada de modelo para uma lista ordenada de alvos e passa ao próximo quando um alvo falha com erro que vale a pena repetir. Este manual mostra como ligar freellmapi, omniroute, bifrost, Anthropic (Claude) e OpenAI em um único `.env`, como executar cada modo e como usar nos exemplos de time de engenharia. A referência de variáveis não é repetida aqui: veja a [seção Router de PROVIDERS.md](../../pt-BR/PROVIDERS.md#router) e o [capítulo 3.6.1 do manual](../../pt-BR/manual/03-modelos-e-provedores.md).

Escopo: o router é uma biblioteca Go usada com `chatmodels.Init("router:<model>")`. Não é um servidor, então VS Code, Cursor ou um console não podem ser apontados para ele; esses clientes são apontados para um gateway (veja [clients-mcp.md](clients-mcp.md#pointing-a-client-at-a-gateway)).

## Os gateways

Cada gateway é um endpoint HTTP compatível com OpenAI, então o router o trata como um alvo com o nome dele e lê `<NOME>_BASE_URL` (e, opcionalmente, `<NOME>_API_KEY`).

| Gateway | Base URL para o router | Chave | Fonte |
|---|---|---|---|
| freellmapi | `http://localhost:3001/v1` (porta padrão 3001) | a chave unificada mostrada no dashboard, começa com `freellmapi-`; o modelo `auto` deixa ele escolher | https://github.com/tashfeenahmed/freellmapi |
| omniroute | `http://localhost:20128/v1` (porta padrão 20128) | a chave do Dashboard, Endpoints; modelo `auto`, `auto/<variante>` ou `provider/model` | https://github.com/diegosouzapw/OmniRoute |
| bifrost | `http://localhost:8080/v1` (porta padrão 8080; `/v1/chat/completions` é o caminho documentado) | check bifrost docs para virtual keys (o quickstart não envia chave); modelo como `provider/model`, por exemplo `openai/gpt-4o-mini` | https://docs.getbifrost.ai/quickstart/gateway/setting-up |
| Anthropic (nativo) | embutido | `ANTHROPIC_API_KEY` | - |
| OpenAI (nativo) | embutido | `OPENAI_API_KEY` | - |

As portas são os padrões indicados nas fontes citadas, sem garantia para a sua instalação: confirme-as na sua (e use as suas se as mudou). O router acrescenta os caminhos OpenAI à base URL, então mantenha o `/v1`.

Os nomes de modelo variam por gateway (`auto` no freellmapi, `provider/model` no bifrost). Para isso existe `ROUTER_MODEL_<NOME>`.

## Um `.env` com os cinco alvos

```bash
# .env
ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
ROUTER_MODE=failover
ROUTER_TIMEOUT=60s

FREELLMAPI_BASE_URL=http://localhost:3001/v1
FREELLMAPI_API_KEY=freellmapi-...
ROUTER_MODEL_FREELLMAPI=auto

OMNIROUTE_BASE_URL=http://localhost:20128/v1
OMNIROUTE_API_KEY=...
ROUTER_MODEL_OMNIROUTE=auto

BIFROST_BASE_URL=http://localhost:8080/v1
# BIFROST_API_KEY=            # only if your bifrost requires one: check bifrost docs
ROUTER_MODEL_BIFROST=openai/gpt-4o-mini

ANTHROPIC_API_KEY=sk-ant-...
ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
OPENAI_API_KEY=sk-...
ROUTER_MODEL_OPENAI=gpt-4o-mini
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // the model after "router:" is used by every target without a ROUTER_MODEL_<NAME>
```

`Init` falha imediatamente, antes de qualquer chamada, quando um alvo está incompleto: `router target "bifrost" needs $BIFROST_BASE_URL`, `router target "anthropic" needs $ANTHROPIC_API_KEY` ou `router target "x" has no model; set ROUTER_MODEL_X or the model after router:`.

## Um exemplo por modo

`ROUTER_MODE` só decide qual alvo é tentado primeiro em cada chamada; os demais vêm na ordem de `ROUTER_TARGETS` se ele falhar.

```bash
# failover (default): always start with freellmapi, fall back down the list
ROUTER_MODE=failover

# round_robin: rotate the first target on every call (the counter lives in one Init call)
ROUTER_MODE=round_robin

# random: a random first target on every call
ROUTER_MODE=random

# weighted: 70% of calls start with freellmapi, 30% with omniroute, bifrost never first
ROUTER_MODE=weighted
ROUTER_WEIGHTS=freellmapi:7,omniroute:3,bifrost:0,anthropic:0,openai:0
```

Um alvo ausente de `ROUTER_WEIGHTS` pesa 1; peso 0 significa nunca primeiro (ele ainda recebe a chamada quando os anteriores falham).

## Modelos por papel no time de engenharia

Dois exemplos, duas formas diferentes de o ambiente chegar ao código.

**`examples/langgraph/independent_engineering_team`** (hub, manager e um contêiner por worker). Cada papel lê `AGENT_MODEL_<PAPEL>`, depois `AGENT_MODEL`, depois os padrões por chave, e passa o valor a `chatmodels.Init`, então `router:` funciona. Papéis: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`. O `.env.example` do exemplo é copiado para `.env`, e todo serviço do `docker-compose.yml` tem `env_file: [{ path: .env, required: false }]`, então todos os contêineres veem o mesmo arquivo, incluindo as variáveis `ROUTER_*`, `*_BASE_URL` e as chaves.

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
# append to that .env: the block from the previous section, plus
AGENT_MODEL=router:gpt-4o-mini
AGENT_MODEL_PO=router:gpt-4o
AGENT_MODEL_ARCHITECT=router:gpt-4o
AGENT_MODEL_JUNIOR=router:gpt-4o-mini
AGENT_MODEL_QA=router:gpt-4o-mini
```

```bash
docker compose -f examples/langgraph/independent_engineering_team/docker-compose.yml up --build
```

O que saber antes de usar:

- Um gateway no host não é `localhost` dentro de um contêiner. Use `http://host.docker.internal:PORT/v1` (Docker Desktop; no Linux adicione `extra_hosts: ["host.docker.internal:host-gateway"]` aos serviços) ou rode o gateway como outro serviço do compose. Confira seu Docker; o exemplo não configura isso.
- A string de modelo do papel passa pelo `normalize` do exemplo, que troca `_` por `-` no modelo e a primeira `/` por `:`. `router:gpt-4o-mini` é seguro. Um modelo como `router:openai/gpt-4o-mini` viraria `router:openai:gpt-4o-mini`; coloque nomes com `/` ou `_` em `ROUTER_MODEL_<NOME>`, que não é normalizado.
- Todos os papéis compartilham um único conjunto de variáveis `ROUTER_*`, então compartilham alvos e modo. Para dar a um papel outro conjunto de alvos seriam necessários processos separados com ambientes separados; o exemplo não faz isso.
- `AGENT_MODEL_<PAPEL>=router:...` conta como definido, então substitui o modelo offline roteirizado; não é preciso `ANTHROPIC_API_KEY` se todos os alvos do router forem gateways.

**`examples/langgraph/engineering_team`** (um processo). Ele não lê `AGENT_MODEL_*`: usa `examples/internal/demo`, que escolhe `anthropic:claude-sonnet-4-5` quando `ANTHROPIC_API_KEY` está definida, `openai:gpt-4o-mini` quando `OPENAI_API_KEY` está definida, senão o modelo roteirizado. Um modelo atende o time inteiro, não há mapeamento por papel e o router não está ligado. Use `independent_engineering_team` para o router. (Para ter o router aqui seria preciso alterar `demo.Smart`; isso é mudança de código, não de configuração.)

A CLI `knowledge` e o hub também aceitam o router: `KNOWLEDGE_MODEL=router:gpt-4o-mini` (lido por `cmd/knowledge`), `AGENT_MODEL_KNOWLEDGE` no exemplo independente. Sob `router:`, o controle de cache do CAG não é aplicado: `cag.CacherFor` usa o prefixo `anthropic:` do `ModelID()`, que aqui é `router:<model>`, então o CAG recorre ao prefixo estável simples.

## Qual alvo respondeu

O próprio router não devolve campo "respondido por", não faz log e `ModelID()` sempre devolve `router:<model>`. Cada provedor alvo chama `models.Track` com o próprio `ModelID()`, então callbacks e tracers (LangSmith, OTLP) registram cada tentativa como `provider:model`, inclusive as que falharam. O que mais dá para observar:

- Quando todos os alvos falham, o erro lista cada um: `router: all targets failed: freellmapi: <error>; omniroute: <error>; ...`.
- Os logs e dashboards dos próprios gateways (freellmapi, omniroute e bifrost têm dashboard ou log de requisições; veja como nas docs deles) mostram quais requisições chegaram.
- Com `ROUTER_MODE=failover`, uma chamada que chega ao segundo alvo significa que o primeiro falhou, mas o router não avisa. Se você precisa de um registro por chamada, hoje o caminho é o log de um gateway ou um teste de timeout/erro contra um gateway falso.

## O que acontece com o contexto numa troca

- **Entre chamadas e no failover.** Todo alvo recebe a mesma lista completa de mensagens e opções, então o histórico da conversa nunca se perde numa troca: o próximo alvo simplesmente recebe o histórico completo de novo. As definições de ferramentas viajam do mesmo modo.
- **Streaming, falha antes de qualquer texto.** O próximo alvo recebe as mensagens originais; quem chamou vê um stream normal.
- **Streaming, falha depois de algum texto (sem tool call).** O próximo alvo recebe as mensagens originais, o texto já emitido como mensagem do assistente e uma mensagem do usuário pedindo para continuar. Quem chamou vê uma resposta contínua. O quão suave fica depende do modelo; ele pode repetir ou reformular.
- **Streaming, falha depois de uma tool call parcial.** Não dá para continuar: o erro é devolvido a quem chamou.
- **Timeout de inatividade.** Em `Stream`, `ROUTER_TIMEOUT` é um timeout de inatividade (tempo até o primeiro trecho, depois o intervalo entre trechos), então uma resposta longa que continua fluindo não é cortada. Em `Generate` limita a chamada inteira.
- Alvos diferentes usam modelos diferentes, então tom, qualidade de tool calling e tamanho de contexto podem mudar depois de uma troca. Mantenha modelos da mesma classe e confirme que todos suportam tool calling se seus agentes usam ferramentas.

## Solução de problemas

| Sintoma | Causa e correção |
|---|---|
| `401` ou `403` devolvido na hora, sem failover | São finais por projeto: o próximo alvo não consertaria uma chave ruim. Corrija a chave do alvo que o erro cita. 400, 404 e outros 4xx também são finais. |
| `router: all targets failed: a: ...; b: ...` | O erro de cada alvo é listado em ordem. Típicos: conexão recusada (gateway parado ou porta errada), 429, 5xx. |
| `router target "bifrost" needs $BIFROST_BASE_URL` no `Init` | Um nome de alvo sem provedor nativo e sem URL padrão na lista OpenAI-compatível embutida (como `groq` ou `openrouter`, que não precisam da variável) precisa de `<NOME>_BASE_URL`; o nome vai para maiúsculas e o que não é alfanumérico vira `_` (`my-gw` -> `MY_GW_BASE_URL`). |
| `set ROUTER_TARGETS to an ordered, comma-separated list` | `ROUTER_TARGETS` está vazio ou não foi exportado para este processo (no Docker, confira o `env_file`). |
| `ROUTER_WEIGHTS: "x" is not in ROUTER_TARGETS` | Os pesos devem citar alvos existentes. |
| Funciona no host, falha no contêiner | `localhost` dentro do contêiner é o próprio contêiner. Veja a nota sobre `host.docker.internal` acima. |
| Um gateway rejeita o nome do modelo | Defina `ROUTER_MODEL_<NOME>` para esse alvo (veja a tabela de gateways). |
| Um alvo muito lento segura todas as chamadas | Defina `ROUTER_TIMEOUT` (por exemplo `30s`); timeout é repetível, então o próximo alvo é tentado. |
