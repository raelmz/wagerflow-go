<div align="center">

<img src="https://capsule-render.vercel.app/api?type=waving&color=0:0d1117,50:1f6feb,100:2ea043&height=160&text=WagerFlow&fontSize=46&fontColor=ffffff&animation=fadeIn&fontAlignY=42" width="100%" />

### Serviço distribuído de processamento de apostas em Go

*Cada operação financeira é processada com garantias de idempotência, concorrência segura e um ledger que nunca mente.*

<br />

[![Status](https://img.shields.io/badge/Status-Em_desenvolvimento-1f6feb?style=for-the-badge&labelColor=0d1117)](#status)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8?style=for-the-badge&labelColor=0d1117&logo=go&logoColor=00ADD8)](#stack)
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
- [Testes](#testes)
- [Uso de IA neste projeto](#uso-de-ia-neste-projeto)

<a id="sobre"></a>
## 🎰 Sobre

Desafio técnico da etapa final do processo seletivo **Backend Developer Júnior — Go** da **Jungle Gaming**.

O **WagerFlow** processa operações financeiras de apostas (`BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`) recebidas via HTTP e AWS SQS, movimentando carteiras de jogadores com garantias equivalentes de correção mesmo sob múltiplas instâncias, reentregas e falhas no meio do processamento. O foco central é integridade financeira: sem duplicidade, sem saldo negativo, sem perda de eventos confirmados.

> 📄 Documentação completa do processo de decisão — arquitetura, trade-offs e justificativas — está em [`docs/PROJETO.md`](./docs/PROJETO.md).

<a id="status"></a>
## 🚀 Status

**Em desenvolvimento** — prazo de entrega: 3 dias corridos.

Checklist detalhado de progresso em [`docs/PROJETO.md`](./docs/PROJETO.md#6-fluxo-de-desenvolvimento-e-plano-dia-a-dia).

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
│   └── api/              → ponto de entrada da aplicação (main.go)
├── internal/
│   ├── domain/           → entidades e regras de negócio, sem dependência de framework
│   ├── application/      → casos de uso, orquestração
│   ├── infrastructure/   → Postgres, SQS, Keycloak, implementações concretas
│   └── interfaces/       → HTTP handlers, consumidores SQS
├── migrations/           → migrations versionadas do banco
├── deployments/
│   └── docker-compose.yml
└── .env.example
```

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="como-rodar"></a>
## ⚙️ Como rodar

```bash
git clone https://github.com/raelmz/wagerflow-go.git
cd wagerflow-go
cp .env.example .env
docker compose -f deployments/docker-compose.yml up -d   # Postgres (LocalStack e Keycloak entram nas próximas etapas)
```

> **Estado atual**: ainda não existe API HTTP (`cmd/api` está vazio — é o próximo bloco de trabalho, com Uber Fx). O que já roda de ponta a ponta contra o banco é o caso de uso de processamento de apostas, hoje exercitado via testes (unitários e de integração, abaixo) e por `cmd/smoketest` (ferramenta descartável de conferência manual, não faz parte da aplicação final).

As migrations em `migrations/` são aplicadas manualmente, uma de cada vez, contra o Postgres do `docker compose` acima:

```bash
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000001_create_wallets_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000002_create_wager_transactions_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000003_create_wallet_ledger_entries_table.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000004_create_inbox_outbox_tables.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000005_wager_reference_and_result.up.sql
docker exec -i wagerflow-postgres psql -U wagerflow -d wagerflow < migrations/000006_ledger_prevent_truncate.up.sql
```

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
# repositório (o docker compose do Postgres deve estar de pé):
#
# No Git Bash (Windows), o MSYS2 converte "/app" para um caminho do
# Windows por engano — por isso o MSYS_NO_PATHCONV=1 na frente.
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/app" -w /app golang:1.27 go test -race ./...

# De integração (build tag "integration"; precisa do Postgres do
# docker compose já rodando — TEST_DATABASE_URL é a conexão
# ADMINISTRATIVA usada só para criar/apagar o banco de cada teste,
# não é o banco da aplicação):
TEST_DATABASE_URL="postgres://wagerflow:wagerflow@localhost:5432/postgres?sslmode=disable" \
  go test -tags=integration -race ./test/integration/...
```

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="uso-de-ia-neste-projeto"></a>
## 🤖 Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos, revisão de decisões técnicas e apoio na implementação. As decisões de arquitetura, escopo e priorização são registradas com justificativa própria em [`docs/PROJETO.md`](./docs/PROJETO.md).

<img src="https://capsule-render.vercel.app/api?type=waving&color=0:0d1117,50:1f6feb,100:2ea043&height=100&section=footer" width="100%" />
