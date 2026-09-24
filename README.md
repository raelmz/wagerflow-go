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
- [Testes](#testes)
- [Uso de IA neste projeto](#uso-de-ia-neste-projeto)

<a id="sobre"></a>
## 🎰 Sobre

Desafio técnico da etapa final do processo seletivo **Backend Developer Júnior — Go** da **Jungle Gaming**.

O **WagerFlow** processa operações financeiras de apostas (`BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`) recebidas via HTTP e AWS SQS, movimentando carteiras de jogadores com garantias equivalentes de correção mesmo sob múltiplas instâncias, reentregas e falhas no meio do processamento. O foco central é integridade financeira: sem duplicidade, sem saldo negativo, sem perda de eventos confirmados.

> 📄 Documentação completa do processo de decisão — arquitetura, trade-offs e justificativas — está em [`docs/PROJETO.md`](./docs/PROJETO.md).

<a id="status"></a>
## 🚀 Status

**Em desenvolvimento** — prazo de entrega: 3 dias corridos (estado em 24/09/2026, noite do dia 2).

| Bloco | Situação |
|---|---|
| Domínio (`Money`, carteira, transação, ledger, eventos de outbox) | ✅ Concluído, com testes unitários |
| Persistência em Postgres (migrations, repositórios, `UnitOfWork`) | ✅ Concluído |
| Processamento de `BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK` com idempotência persistente | ✅ Concluído |
| Outbox transacional (**escrita** dos eventos na mesma transação) | ✅ Concluído |
| Testes de integração contra Postgres real (seção 13 do desafio) | ✅ Concluído |
| API HTTP (chi) + composição com Uber Fx | ✅ Rotas implementadas; falta cobertura de testes de handler |
| Autenticação real (Keycloak / OIDC) — **requisito eliminatório** | ✅ Concluído — Keycloak provisionado no compose, isolamento entre provedores e restrição de operações internas |
| Publicação da outbox + consumidor SQS + inbox | ⏳ Não iniciado |
| Worker de referências pendentes | ⏳ Não iniciado |
| Observabilidade, `Dockerfile`, `docker compose up` completo | ⏳ Não iniciado |

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
│   ├── api/              → ponto de entrada da API (main.go) — único lugar que conhece o Uber Fx
│   └── smoketest/        → conferência manual descartável (não faz parte da aplicação final)
├── internal/
│   ├── config/           → leitura das variáveis de ambiente
│   ├── domain/           → entidades e regras de negócio, sem dependência de framework
│   ├── application/      → casos de uso, orquestração
│   ├── infrastructure/   → Postgres (hoje); SQS entra na próxima etapa
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

```bash
git clone https://github.com/raelmz/wagerflow-go.git
cd wagerflow-go
cp .env.example .env
docker compose -f deployments/docker-compose.yml up -d   # Postgres + Keycloak (LocalStack entra na próxima etapa)
```

> **Estado atual**: a API HTTP já sobe (`go run ./cmd/api`) com autenticação real via Keycloak. `cmd/smoketest` é uma ferramenta descartável de conferência manual, não faz parte da aplicação final.

O Keycloak demora um pouco mais que o Postgres para ficar pronto na primeira vez (baixa a imagem e importa o realm). Confira com `docker ps` até os dois containers aparecerem como `healthy`.

As migrations em `migrations/` são aplicadas manualmente, uma de cada vez, contra o Postgres do `docker compose` acima (**antes** de subir a API):

```bash
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000001_create_wallets_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000002_create_wager_transactions_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000003_create_wallet_ledger_entries_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000004_create_inbox_outbox_tables.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000005_wager_reference_and_result.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000006_ledger_prevent_truncate.up.sql
```

Depois das migrations, suba a API (o `.env` é carregado automaticamente; `DATABASE_URL` é obrigatória e `HTTP_PORT` tem padrão `8080`):

```bash
go run ./cmd/api
```

```bash
curl http://localhost:8080/health/live
```

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
| `GET /health/live` · `GET /health/ready` | Liveness (processo) e readiness (Postgres) |

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

<a id="testes"></a>
## 🧪 Testes

O projeto tem duas camadas de teste, com propósitos diferentes:

- **Unitários** (`internal/domain`, `internal/application`): rápidos, sem Docker, usando repositórios em memória (`fakes_test.go`). Cobrem as regras de negócio — máquina de estados, idempotência, `Money`, reversões — mas **não** provam concorrência real nem constraints do banco.
- **De integração** (`test/integration/`, build tag `integration`): rodam contra um Postgres real, cada teste com um banco isolado (criado e apagado na hora, com as migrations aplicadas do zero). É aqui que a seção 13 do desafio é provada de verdade: as duas apostas de 80.00 sobre saldo de 100.00, a mesma aposta 50× em paralelo, carteiras diferentes em paralelo, duas reversões concorrentes, imutabilidade do ledger (incluindo `TRUNCATE`), idempotência sobrevivendo a um "reinício" do processo, atomicidade com erro/panic, e atomicidade da outbox.

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
