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
docker compose -f deployments/docker-compose.yml up -d   # Postgres, LocalStack, Keycloak
go run ./cmd/api
```

<a id="testes"></a>
## 🧪 Testes

```bash
go test ./...
go test -race ./...
go vet ./...
```

<img src="https://capsule-render.vercel.app/api?type=rect&color=0:0d1117,50:1f6feb,100:2ea043&height=4&section=header" width="100%" />

<a id="uso-de-ia-neste-projeto"></a>
## 🤖 Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos, revisão de decisões técnicas e apoio na implementação. As decisões de arquitetura, escopo e priorização são registradas com justificativa própria em [`docs/PROJETO.md`](./docs/PROJETO.md).

<img src="https://capsule-render.vercel.app/api?type=waving&color=0:0d1117,50:1f6feb,100:2ea043&height=100&section=footer" width="100%" />
