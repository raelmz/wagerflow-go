<div align="center">

<img src="https://capsule-render.vercel.app/api?type=waving&color=0:0d1117,50:1f6feb,100:2ea043&height=160&text=WagerFlow&fontSize=46&fontColor=ffffff&animation=fadeIn&fontAlignY=42" width="100%" />

### Serviço distribuído de processamento de apostas em Go

*Cada operação financeira é processada com garantias de idempotência, concorrência segura e um ledger que nunca mente.*

<br />

[![Status](https://img.shields.io/badge/Status-Em_desenvolvimento-1f6feb?style=for-the-badge&labelColor=0d1117)](#status)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&labelColor=0d1117&logo=go&logoColor=00ADD8)](#stack)
[![Processo](https://img.shields.io/badge/Jungle_Gaming-Backend_Jr-2ea043?style=for-the-badge&labelColor=0d1117)](#sobre)

<br />

<a href="./docs/PROJETO.md"><b>📖 Documentação de decisões</b></a>
&nbsp;•&nbsp;
<a href="./docs/DESAFIO.md"><b>📋 Desafio original</b></a>
&nbsp;•&nbsp;
<a href="#como-rodar"><b>⚙️ Rodar localmente</b></a>

</div>

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

## Índice

- [Sobre](#sobre)
- [Status](#status)
- [Stack](#stack)
- [Estrutura do repositório](#estrutura-do-repositório)
- [Como rodar](#como-rodar)
- [Autenticação](#autenticação)
- [API HTTP](#api-http)
- [Mensageria (SQS)](#mensageria-sqs)
- [Testes](#testes)
- [Uso de IA neste projeto](#uso-de-ia-neste-projeto)

<a id="sobre"></a>
## 🎰 Sobre

Desafio técnico da etapa final do processo seletivo **Backend Developer Júnior — Go** da **Jungle Gaming**.

O **WagerFlow** processa operações financeiras de apostas (`BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`) recebidas via HTTP e AWS SQS, movimentando carteiras de jogadores com garantias equivalentes de correção mesmo sob múltiplas instâncias, reentregas e falhas no meio do processamento. O foco central é integridade financeira: sem duplicidade, sem saldo negativo, sem perda de eventos confirmados.

> 📄 Documentação completa do processo de decisão — arquitetura, trade-offs e justificativas — está em [`docs/PROJETO.md`](./docs/PROJETO.md).

<a id="status"></a>
## 🚀 Status

**Todos os critérios eliminatórios e a entrega básica (`docker compose up --build`) estão concluídos e validados** — o que resta é ampliar cobertura de teste e observabilidade (nota extra, sem bloqueador de prazo).

| Bloco | Situação |
|---|---|
| Domínio (`Money`, carteira, transação, ledger, eventos de outbox) | ✅ Concluído, com testes unitários |
| Persistência em Postgres (migrations, repositórios, `UnitOfWork`) | ✅ Concluído |
| Processamento de `BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK` com idempotência persistente | ✅ Concluído |
| Outbox transacional (**escrita** dos eventos na mesma transação) | ✅ Concluído |
| Testes de integração contra Postgres real (seção 13 do desafio) | ✅ Concluído |
| API HTTP (chi) + composição com Uber Fx | ✅ Rotas implementadas; falta cobertura de testes de handler |
| Autenticação real (Keycloak / OIDC) — **requisito eliminatório** | ✅ Concluído — Keycloak provisionado no compose, isolamento entre provedores e restrição de operações internas |
| Publicação da outbox (`cmd/outbox-publisher`) | ✅ Concluído — testes de integração (concorrência, backoff, recuperação de lock) e validação manual ponta a ponta contra Postgres/SQS reais |
| Consumidor SQS (`cmd/wager-consumer`) + inbox | ✅ Concluído — testes de integração e validação manual ponta a ponta contra Postgres/SQS reais |
| Worker de referências pendentes (`cmd/pending-reference-worker`) | ✅ Concluído, com testes unitários (fakes) — **ainda não executado contra Postgres real** (sem teste de integração automatizado; ver limitações) |
| `Dockerfile`, `docker compose up --build` completo | ✅ Concluído, validado de ponta a ponta |
| Observabilidade (logs JSON + `/health/ready` completo) | ✅ Logs estruturados (`log/slog`) e `/health/ready` cobrindo Postgres+SQS+Keycloak concluídos e validados — **métricas ainda pendentes** (ver limitações) |

O que ficou de fora e por quê está detalhado, com honestidade, em [`docs/PROJETO.md`](./docs/PROJETO.md#5-limitações-conhecidas-e-trabalho-não-concluído). Checklist por dia em [`docs/PROJETO.md`](./docs/PROJETO.md#6-fluxo-de-desenvolvimento-e-plano-dia-a-dia).

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="stack"></a>
## 🛠️ Stack

<div align="center">

![Go](https://img.shields.io/badge/Go-0d1117?style=for-the-badge&logo=go&logoColor=00ADD8)
![Uber Fx](https://img.shields.io/badge/Uber_Fx-0d1117?style=for-the-badge&logo=uber&logoColor=ffffff)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-0d1117?style=for-the-badge&logo=postgresql&logoColor=2ea043)
![Docker](https://img.shields.io/badge/Docker-0d1117?style=for-the-badge&logo=docker&logoColor=1f6feb)
![AWS SQS](https://img.shields.io/badge/AWS_SQS-0d1117?style=for-the-badge&logo=amazonsqs&logoColor=ff9900)
![Keycloak](https://img.shields.io/badge/Keycloak-0d1117?style=for-the-badge&logo=keycloak&logoColor=ffffff)

</div>

Decisões de arquitetura e justificativa de cada escolha em [`docs/PROJETO.md`](./docs/PROJETO.md).

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="estrutura-do-repositório"></a>
## 📂 Estrutura do repositório

```
wagerflow-go/
├── docs/
│   ├── DESAFIO.md        → enunciado original do desafio
│   └── PROJETO.md        → decisões de arquitetura, com justificativas
├── cmd/
│   ├── api/                 → ponto de entrada da API (main.go) — único lugar que conhece o Uber Fx
│   ├── outbox-publisher/    → publica outbox_events pendentes no SQS (multi-instância, sem Fx)
│   ├── wager-consumer/      → consome wager-transactions.fifo, com inbox (multi-instância, sem Fx)
│   ├── pending-reference-worker/ → reaplica WagerTransaction em PENDING_REFERENCE (backoff, TTL, multi-instância, sem Fx)
│   └── smoketest/           → conferência manual descartável (não faz parte da aplicação final)
├── internal/
│   ├── config/           → leitura das variáveis de ambiente
│   ├── domain/           → entidades e regras de negócio, sem dependência de framework
│   ├── application/      → casos de uso, orquestração (processamento HTTP e consumo SQS compartilham as mesmas regras)
│   ├── infrastructure/   → Postgres e SQS (messaging/)
│   └── interfaces/http/  → router chi, handlers, DTOs, middleware de auth (Keycloak/OIDC) e mapeamento de erros para status HTTP
├── migrations/           → migrations versionadas do banco
├── test/integration/     → testes contra Postgres real (build tag `integration`)
├── deployments/
│   ├── docker-compose.yml
│   └── keycloak/realm-export.json → realm provisionado automaticamente no boot do Keycloak
└── .env.example
```

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="como-rodar"></a>
## ⚙️ Como rodar

A forma recomendada é subir **tudo** (Postgres, Keycloak, LocalStack, migrations e os 4 binários Go) com um único comando:

```bash
git clone https://github.com/raelmz/wagerflow-go.git
cd wagerflow-go
cp .env.example .env
docker compose -f deployments/docker-compose.yml up --build
```

Isso sobe, nesta ordem (o `depends_on`/`healthcheck` de cada serviço garante a ordem certa sozinho):

1. `postgres`, `keycloak`, `localstack` — sobem em paralelo, cada um até ficar `healthy`.
2. `migrate` — aplica todas as migrations pendentes de `migrations/` contra o Postgres do compose e **termina** (não fica no ar). Usa a imagem oficial `migrate/migrate`, não precisa instalar nada.
3. `api`, `outbox-publisher`, `wager-consumer`, `pending-reference-worker` — os 4 binários do projeto, cada um construído a partir do `Dockerfile` na raiz do repositório (mesmo Dockerfile para os 4, variando só o binário copiado — ver comentários no arquivo). Todos só sobem depois do `migrate` terminar com sucesso.

Primeira vez demora mais (build das imagens Go + download de Postgres/Keycloak/LocalStack). Acompanhe os logs no próprio terminal; `Ctrl+C` derruba tudo. Para rodar em segundo plano, use `up --build -d` e `docker compose -f deployments/docker-compose.yml logs -f <serviço>` para acompanhar um serviço específico.

```bash
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
```

`ready` confere Postgres, SQS e Keycloak e retorna todas as falhas de uma vez, não só a primeira. Os 4 binários (`api`, `outbox-publisher`, `wager-consumer`, `pending-reference-worker`) emitem logs estruturados em JSON no `stdout` (via `log/slog`), com `correlationId` reaproveitado dos eventos de negócio — inclusive um log por requisição HTTP.

> As filas SQS (`wagerflow-events.fifo`, `wager-transactions.fifo`) não são criadas pelo compose: o próprio `outbox-publisher`/`wager-consumer` as cria (via `CreateQueue`, idempotente) na primeira vez que rodam contra o LocalStack.

**Rodando fora do Docker** (útil para debugar um binário isolado com `go run`, ou testar com `-race`): suba só a infra —

```bash
docker compose -f deployments/docker-compose.yml up -d postgres keycloak localstack
./deployments/migrate.sh up
go run ./cmd/api
```

Nesse caso o `.env` já tem `DATABASE_URL`/`KEYCLOAK_ISSUER_URL`/`SQS_ENDPOINT_URL` apontando para `localhost` (as portas publicadas pelo compose), diferente das variáveis internas (`postgres`, `keycloak`, `localstack`) que os serviços `api`/`outbox-publisher`/`wager-consumer`/`pending-reference-worker` usam quando rodam DENTRO do compose — cada ambiente enxerga a infra pelo nome que faz sentido para ele.

`cmd/smoketest` é uma ferramenta descartável de conferência manual, não faz parte da aplicação final e não tem serviço próprio no compose.

<a id="autenticação"></a>
## 🔐 Autenticação

Todas as rotas de negócio exigem um access token OAuth2/OIDC válido (`Authorization: Bearer <token>`), emitido pelo Keycloak via `client_credentials`. Só `GET /health/live` e `GET /health/ready` ficam abertos.

O realm `wagerflow` já vem provisionado no `docker compose` (`deployments/keycloak/realm-export.json`) com 3 clients de exemplo — o `client_id` de cada um É o `providerId` que a API reconhece:

| `client_id` | `client_secret` (dev) | Role | Pode chamar |
|---|---|---|---|
| `provider-a` | `provider-a-secret` | `provider` | Rotas de wagering, só das próprias transações (`providerId` do token precisa bater com o da requisição) |
| `provider-b` | `provider-b-secret` | `provider` | Idem, para `provider-b` |
| `wagerflow-internal` | `internal-secret` | `internal` | Rotas de wagering (sem checagem de `providerId`) **e** rotas de carteira (`/wallets/...`) |

Pegando um token (Keycloak publicado em `http://localhost:8081`):

```bash
curl -X POST http://localhost:8081/realms/wagerflow/protocol/openid-connect/token \
  -d "grant_type=client_credentials" \
  -d "client_id=provider-a" \
  -d "client_secret=provider-a-secret"
```

O token dura 300s (`expires_in`). Use o valor de `access_token` da resposta:

```bash
curl -i http://localhost:8080/wagering/transactions/00000000-0000-0000-0000-000000000000 \
  -H "Authorization: Bearer SEU_TOKEN_AQUI"
```

Respostas de erro relacionadas à auth:

| Status | `code` | Quando |
|---|---|---|
| `401` | `MISSING_CREDENTIALS` | Sem header `Authorization`, ou sem o prefixo `Bearer ` |
| `401` | `INVALID_CREDENTIALS` | Assinatura, issuer ou formato do token inválidos |
| `401` | `EXPIRED_CREDENTIALS` | Token expirado |
| `403` | `FORBIDDEN` | Token válido, mas sem o role exigido pela rota |
| `403` | `PROVIDER_MISMATCH` | Token válido e com o role certo, mas o `providerId` do token não bate com o da requisição (corpo ou URL) |

Consultar uma transação de OUTRO provedor pelo id interno (`GET /wagering/transactions/{id}`, sem `providerId` na URL) devolve `404`, não `403` — evita confirmar para um provedor que aquele id existe e pertence a outro (detalhes e justificativa em [`docs/PROJETO.md`](./docs/PROJETO.md#47-autenticação-e-autorização-keycloakoidc)).

<a id="api-http"></a>
## 🌐 API HTTP

Router [chi](https://github.com/go-chi/chi), composição com [Uber Fx](https://uber-go.github.io/fx/). Dinheiro trafega sempre como **string decimal** (`"25.00"`), nunca como número JSON. Cada requisição carrega um `X-Correlation-Id` (gerado se ausente, devolvido na resposta), que também vai para os eventos da outbox.

| Método e rota | O que faz |
|---|---|
| `POST /wallets` | Abre carteira (com saldo inicial opcional). `409` se já existe carteira do jogador na moeda |
| `GET /wallets/{walletId}` | Consulta carteira e saldo |
| `GET /wallets/{walletId}/ledger?cursor=&limit=` | Ledger paginado (cursor opaco, ordem estável `createdAt`, `id`) |
| `POST /wallets/{walletId}/reconciliation` | Reconstrói o saldo a partir do ledger e compara com o saldo armazenado (só leitura) |
| `POST /wagering/transactions` | Processa `BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK`. Header `Idempotency-Key` **obrigatório** |
| `GET /wagering/transactions/{transactionId}` | Consulta transação pelo id interno |
| `GET /providers/{providerId}/wagering/transactions/{externalTransactionId}` | Consulta por provedor + id externo |
| `GET /health/live` · `GET /health/ready` | Liveness (processo) e readiness (Postgres + SQS + Keycloak) |

**Status HTTP** (o contrato distingue cada situação, como pede a seção 9 do desafio):

| Status | Quando |
|---|---|
| `200` | Replay idempotente, ou operação avaliada e `REJECTED` (a recusa é resultado de negócio, não erro HTTP) |
| `201` | Operação nova processada com sucesso (`PROCESSED`), ou carteira criada |
| `202` | Operação nova aguardando a referência (`PENDING_REFERENCE`) |
| `400` | Entrada inválida (JSON malformado, UUID ruim, valor inválido, header ausente) |
| `404` | Carteira ou transação não encontrada |
| `409` | Conflito: chave de idempotência reutilizada com payload diferente, id externo já usado por outra chave, carteira duplicada |
| `422` | Dado coerente, mas a regra de negócio recusa (jogador que não é dono da carteira, moeda diferente da carteira) |
| `503` | Falha transitória (banco indisponível etc.) — pode repetir com a mesma chave sem efeito duplicado |

A tabela completa, com os códigos de erro do corpo (`code`), está em `internal/interfaces/http/errors.go` e a justificativa em [`docs/PROJETO.md`](./docs/PROJETO.md#46-api-http).

<a id="mensageria-sqs"></a>
## 📨 Mensageria (SQS)

Dois binários adicionais, sem Fx (poucas dependências cada, não compensa DI):

- **`cmd/outbox-publisher`** — publica os eventos gravados na outbox (seção 4.4 do `docs/PROJETO.md`) na fila `wagerflow-events.fifo`. Multi-instância via `SELECT ... FOR UPDATE SKIP LOCKED`.
- **`cmd/wager-consumer`** — consome `wager-transactions.fifo`, o canal de entrada assíncrono de operações (equivalente a `POST /wagering/transactions`, **mesmo caso de uso**: `ProcessWagerCommand`). Corpo esperado da mensagem:

```json
{
  "idempotencyKey": "...",
  "providerId": "provider-a",
  "externalTransactionId": "...",
  "playerId": "uuid",
  "walletId": "uuid",
  "roundId": "...",
  "gameId": "...",
  "kind": "BET",
  "money": { "amount": "25.00", "currency": "BRL" },
  "referenceExternalTransactionId": ""
}
```

Deduplicação em duas camadas: a tabela `inbox_messages` (por `consumerName` + `messageId` do SQS, gravada na MESMA transação do efeito financeiro) pega reentregas da mesma entrega lógica; a idempotência do domínio (`idempotencyKey`/`externalTransactionId`) pega a mesma operação chegando em mensagens diferentes. Erro de validação/conflito (permanente) manda a mensagem direto para `wager-transactions-dlq.fifo`; erro de infraestrutura (transitório) só não apaga a mensagem — ela volta pela `VisibilityTimeout` e tenta de novo sozinha, com o `maxReceiveCount` da fila como rede de segurança.

Ambos os binários rodam com `go run ./cmd/outbox-publisher` / `go run ./cmd/wager-consumer`, contra o LocalStack do `docker compose` (`SQS_ENDPOINT_URL=http://localhost:4566`). As filas (e a DLQ) são criadas automaticamente no boot — não há passo de provisionamento manual.

### 🔁 Worker de referências pendentes

`cmd/pending-reference-worker` reivindica `WagerTransaction` em `PENDING_REFERENCE` (REFUND/ROLLBACK/WIN que chegaram antes da operação que referenciam — seção 7 do desafio) e as reaplica, reaproveitando a MESMA regra de negócio do processamento HTTP/SQS. Cada transação tem **5 tentativas OU 5 minutos** (o que vier primeiro) desde que entrou em `PENDING_REFERENCE`; esgotado o limite, é rejeitada definitivamente com `failureCode = REFERENCE_NOT_FOUND`. Suporta múltiplas instâncias (mesmo mecanismo de lock com `SELECT ... FOR UPDATE SKIP LOCKED` do `outbox-publisher`). Roda com:

```
go run ./cmd/pending-reference-worker
```

Ver detalhes de design em [`docs/PROJETO.md` seção 5.2](./docs/PROJETO.md#52-worker-de-referências-pendentes-implementado-em-25092026).

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="testes"></a>
## 🧪 Testes

O projeto tem duas camadas de teste, com propósitos diferentes:

- **Unitários** (`internal/domain`, `internal/application`): rápidos, sem Docker, usando repositórios em memória (`fakes_test.go`). Cobrem as regras de negócio — máquina de estados, idempotência, `Money`, reversões — mas **não** provam concorrência real nem constraints do banco.
- **De integração** (`test/integration/`, build tag `integration`): rodam contra um Postgres real, cada teste com um banco isolado (criado e apagado na hora, com as migrations aplicadas do zero). É aqui que a seção 13 do desafio é provada de verdade: as duas apostas de 80.00 sobre saldo de 100.00, a mesma aposta 50× em paralelo, carteiras diferentes em paralelo, duas reversões concorrentes, imutabilidade do ledger (incluindo `TRUNCATE`), idempotência sobrevivendo a um "reinício" do processo, atomicidade com erro/panic, e atomicidade da outbox. `wager_consumer_integration_test.go` cobre o consumidor SQS: mensagem nova, reentrega da mesma mensagem (mesmo `messageId`, não duplica), mesma operação por mensagens diferentes (vira replay), e mensagens concorrentes da mesma operação (só um débito). **Executados e confirmados `ok`** contra Postgres real via Docker (ver comandos abaixo); o worker de referências pendentes (`cmd/pending-reference-worker`) é a única exceção — só tem testes unitários com fakes, ainda sem teste de integração automatizado (ver `docs/PROJETO.md`, seção 5.2).

```bash
# Unitários (não precisam de banco)
go test ./...
go vet ./...

# -race precisa de cgo + um compilador C. No Windows isso costuma faltar
# (erro "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1").
# O jeito mais simples de contornar é rodar dentro de um container Linux,
# que já traz gcc. Rode este comando inteiro, numa linha só, na raiz do
# repositório:
#
# No Git Bash (Windows), o MSYS2 converte "/app" para um caminho do
# Windows por engano — por isso o MSYS_NO_PATHCONV=1 na frente.
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/app" -w /app golang:1.27 go test -race ./...

# De integração (build tag "integration"; precisa do Postgres do
# docker compose já rodando — TEST_DATABASE_URL é a conexão
# ADMINISTRATIVA usada só para criar/apagar o banco de cada teste,
# não é o banco da aplicação).
#
# Em Linux/macOS (com cgo disponível), direto:
TEST_DATABASE_URL="postgres://wagerflow:wagerflow@localhost:5432/postgres?sslmode=disable" \
  go test -tags=integration -race ./test/integration/...

# No Windows (Git Bash), pelo container, na mesma rede do compose. O
# compose fica em deployments/, então a rede se chama
# "deployments_default" (confira com `docker network ls`):
MSYS_NO_PATHCONV=1 docker run --rm --network deployments_default \
  -e TEST_DATABASE_URL="postgres://wagerflow:wagerflow@wagerflow-postgres:5432/postgres?sslmode=disable" \
  -v "$(pwd -W):/app" -w /app golang:1.27 \
  go test -tags=integration -race ./test/integration/...
```

> ⚠️ A camada HTTP (`internal/interfaces/http`) ainda **não tem testes automatizados de handler** (wallet/wager): hoje é coberta só indiretamente (casos de uso e integração) e por conferência manual. O middleware de autenticação/autorização é exceção — tem teste próprio (`auth_middleware_test.go`), com um `TokenVerifier` fake, sem precisar do Keycloak real.

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="uso-de-ia-neste-projeto"></a>
## 🤖 Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos, revisão de decisões técnicas e apoio na implementação. As decisões de arquitetura, escopo e priorização são registradas com justificativa própria em [`docs/PROJETO.md`](./docs/PROJETO.md).

<img src="https://capsule-render.vercel.app/api?type=waving&color=0:0d1117,50:1f6feb,100:2ea043&height=100&section=footer" width="100%" />
