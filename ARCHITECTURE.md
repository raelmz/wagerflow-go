# ARCHITECTURE.md — wagerflow-go

> Este documento resume as decisões de arquitetura exigidas pela seção 15 do
> desafio (dinheiro, transações, idempotência, locks, referências pendentes,
> reversões, inbox/outbox, autenticação, autorização, uso do Fx e shutdown),
> limitações conhecidas, interpretações adotadas e trabalho não concluído. É
> um resumo executivo — o histórico completo de cada decisão, com o
> "porquê" detalhado e o changelog dia a dia, está em
> [`docs/PROJETO.md`](./docs/PROJETO.md), que serviu de base para este
> arquivo.

## 1. Visão geral

Serviço de processamento distribuído de apostas (wallet/ledger de iGaming)
em Go. Recebe operações financeiras (`BET`, `WIN`, `LOSS`, `REFUND`,
`ROLLBACK`) por dois canais equivalentes — HTTP e SQS —, com idempotência
persistente, concorrência seguindo sem lock global e ledger auditável
append-only.

Quatro binários independentes, todos compartilhando o mesmo domínio e a
mesma camada de aplicação:

| Binário | Responsabilidade | Composição |
|---|---|---|
| `cmd/api` | API HTTP (abertura de carteira, envio/consulta de operações, reconciliação, health) | **Uber Fx** |
| `cmd/outbox-publisher` | Publica eventos da outbox no SQS | Sem Fx (poucas dependências) |
| `cmd/wager-consumer` | Consome `wager-transactions.fifo`, aplica a MESMA regra de negócio do HTTP | Sem Fx |
| `cmd/pending-reference-worker` | Reaplica transações em `PENDING_REFERENCE` (backoff + TTL) | Sem Fx |

## 2. Estrutura de pastas (DDD simplificado)

```
internal/
  domain/          # Money, Wallet, WagerTransaction, WalletLedgerEntry — sem dependência de infra
  application/      # Casos de uso, UnitOfWork/TxRunner (interfaces), payload hash, backoff
  infrastructure/    # postgres (pgx/v5), messaging (SQS/LocalStack)
  interfaces/http/   # chi, handlers, middleware de auth, mapeamento de erro → status HTTP
  bootstrap/         # composição do Uber Fx da API (único lugar que conhece o Fx)
```

`domain` e `application` nunca importam `infrastructure` diretamente — só
interfaces (`UnitOfWork`, repositórios). A implementação concreta
(Postgres, SQS) é injetada em `cmd/*/main.go`, o único lugar que conhece
Uber Fx e os detalhes de infraestrutura.

## 3. Dinheiro (`Money`)

`Money` é representado como `int64` em **centavos**, nunca `float`, para
evitar erro de arredondamento em valor financeiro (critério eliminatório).
O construtor a partir de string recalcula a aritmética para validar o
formato; `Add`/`Subtract`/`Negate` detectam overflow.

## 4. Transações e máquina de estados

`WagerTransaction` tem estados com guards de transição; os estados
terminais são `PROCESSED`, `REJECTED` e `FAILED`. `OPENING` só pode ser
criado internamente (construtor separado) — o desafio exige rejeitar
`OPENING` vindo de fora.

Toda transação grava, junto com o estado final, o **saldo resultante**
(`resulting_balance_cents`) e a **referência resolvida**
(`reference_transaction_id`), para que um replay devolva o saldo da época
em vez de recalcular contra o estado atual da carteira.

Rejeição de negócio é gravada e commitada como `REJECTED` +
`failureCode` — **não é rollback de transação SQL**, é um resultado válido
e auditável, para manter o replay estável. Entradas inválidas (valor mal
formado, carteira inexistente, jogador que não é dono, moeda diferente da
carteira) nunca chegam a virar `WagerTransaction`: são erro de validação,
sem efeito persistido.

## 5. Idempotência

Hash canônico (JSON com chaves ordenadas + SHA-256) do payload de negócio,
calculado em `application/payload_hash.go` e usado igualmente pelos
caminhos HTTP e SQS — exclui a chave de idempotência e campos de
transporte, para que o mesmo payload gere o mesmo hash nos dois canais.

Corrida entre duas requisições idênticas é decidida por **índice único no
Postgres**: o perdedor recebe `ErrDuplicateTransaction` (SQLSTATE 23505) e
relê o vencedor **numa transação nova** (um erro de `INSERT` aborta a
transação Postgres corrente, então a releitura não pode reusar a mesma
transação).

## 6. Concorrência e locks

- **Saldo da carteira**: update atômico condicionado
  (`UPDATE ... WHERE balance_cents >= X`), sem lock explícito — a própria
  cláusula `WHERE` do Postgres garante a invariante.
- **Reversões (REFUND/ROLLBACK)**: `SELECT ... FOR UPDATE` na linha da
  transação de referência, mais um índice único parcial
  (`uq_wager_single_processed_reversal`) — serializa por referência, sem
  lock global no sistema.
- **Workers (outbox-publisher, pending-reference-worker)**:
  `SELECT ... FOR UPDATE SKIP LOCKED` + coluna de lock lógico
  (`locked_by`/`locked_at`, com timeout de abandono) — suporta múltiplas
  instâncias concorrentes do mesmo binário sem coordenação externa, e
  recupera trabalho de uma instância que morreu no meio.

## 7. Referências pendentes

Quando a referência de uma operação (ex.: a `BET` de um `WIN`) não existe
ainda, ou existe mas está `PENDING`/`PENDING_REFERENCE`, a transação entra
em `PENDING_REFERENCE` e aguarda. Se a referência já terminou como
`REJECTED`/`FAILED`, a transação é rejeitada imediatamente
(`REFERENCE_NOT_PROCESSED`) — só se espera quando a referência ainda pode
virar `PROCESSED`.

O `cmd/pending-reference-worker` reivindica lotes (`Claim`) e reaplica a
MESMA função `applyWagerTransaction` usada por HTTP/SQS/primeira
tentativa — zero duplicação de regra. Limite confirmado com o
desenvolvedor: **5 tentativas OU 5 minutos desde a primeira vez que a
transação entrou em `PENDING_REFERENCE`, o que vier primeiro**; ao
esgotar, rejeição definitiva com `REFERENCE_NOT_FOUND`, pela mesma função
`reject()` de qualquer outra rejeição.

## 8. Reversões (REFUND/ROLLBACK)

**Interpretação adotada**: uma referência aceita, no máximo, **uma**
reversão bem-sucedida, de qualquer tipo — um `ROLLBACK` depois de um
`REFUND` da mesma `BET` é rejeitado. `ROLLBACK` de uma `BET` é crédito
(devolve a aposta); `ROLLBACK` de `WIN`/`REFUND` é débito (desfaz o que
tinha sido creditado). `REFUND` só se aplica a `BET`; `ROLLBACK` se aplica
a `BET`/`WIN`/`REFUND`. Essa regra impede a devolução duplicada do mesmo
débito.

## 9. Inbox e outbox

- **Outbox (escrita)**: o evento é gravado na MESMA transação SQL do
  efeito financeiro (`UnitOfWork.Outbox().Append`), no ponto exato onde o
  estado final é persistido — nunca existe evento órfão nem fato sem
  evento correspondente.
- **Outbox (publicação)**: `cmd/outbox-publisher`, binário separado (o
  desafio exige suportar múltiplos publishers), reivindica lotes com
  `SELECT ... FOR UPDATE SKIP LOCKED`, publica no SQS com backoff
  exponencial e preserva o `eventId` original em republicações.
- **Inbox**: gravada na MESMA transação SQL do efeito financeiro do lado
  do consumidor (`InboxRepository.TryInsert`,
  `ON CONFLICT ... DO NOTHING`, decidido por `RowsAffected()`) — dedup
  atômico contra reentrega do SQS (at-least-once), sob múltiplas
  instâncias do consumidor.
- **Duas camadas de deduplicação, propositais**: a inbox pega reentrega da
  MESMA entrega SQS (`consumerName` + `messageId`); a idempotência de
  domínio (`idempotencyKey`/`externalTransactionId`) pega a MESMA operação
  de negócio chegando em **mensagens diferentes**. São problemas
  distintos.
- **Erro permanente vs. transitório** no consumidor: permanente
  (`IsPermanentWagerError`) vai explícita e imediatamente para a DLQ, sem
  esperar o redrive automático; transitório apenas não apaga a mensagem,
  que volta pela `VisibilityTimeout` — o `maxReceiveCount` da
  `RedrivePolicy` é a rede de segurança.

## 10. Autenticação e autorização (Keycloak/OIDC)

Todas as rotas de negócio exigem `Authorization: Bearer <token>` validado
contra o Keycloak (`coreos/go-oidc` + `golang-jwt`); só os health checks
ficam abertos. O realm `wagerflow` é provisionado automaticamente pelo
`docker compose` (`deployments/keycloak/realm-export.json`), com 3
clients de `client_credentials`: `provider-a` e `provider-b` (role
`provider`) e `wagerflow-internal` (role `internal`).

O `client_id` de cada client **é** o `providerId` que a API reconhece — o
isolamento entre provedores é feito comparando o claim `azp` do token com
o `providerId` da requisição (URL ou corpo), retornando `403
PROVIDER_MISMATCH` quando não bate. Consultar uma transação de outro
provedor por id interno devolve `404` (não `403`), para não confirmar a
um provedor que aquele id existe e pertence a outro. Rotas de carteira
(`/wallets/...`) exigem role `internal`.

## 11. Uber Fx

Fx é usado **só** na composição de `cmd/api` — é onde o desafio pede
(seção 3), e é o binário com dependências suficientes (config, pool,
repositórios, casos de uso, verificador de token, router) para um
container de DI valer a pena. Os outros 3 binários (`outbox-publisher`,
`wager-consumer`, `pending-reference-worker`) têm poucas dependências
cada um e montam o grafo manualmente em `main.go` — Fx ali seria
complexidade sem ganho real.

A composição em si (a lista de `fx.Provide`/`fx.Invoke`) mora em
`internal/bootstrap/api_module.go`, exportada como `bootstrap.Module`,
e não em `cmd/api/main.go` — que fica só com `fx.New(bootstrap.Module).Run()`.
A razão é puramente técnica: um pacote `main` não pode ser importado
por nenhum outro pacote em Go, nem por um teste, e a seção 13 do
desafio exige um teste que monte essa composição e verifique
`Start`/`Stop`. `test/integration/fx_lifecycle_integration_test.go`
monta o MESMO `bootstrap.Module` de produção, confirma que o `Start`
sobe o servidor de verdade e que o `Stop` libera os recursos (pool do
Postgres fechado, servidor HTTP não aceita mais conexão).

## 12. Shutdown

Todos os binários fora do Fx usam `signal.NotifyContext` (SIGTERM/SIGINT)
para cancelar o `context.Context` do loop principal — "pare de reivindicar
lote novo, termine o que já está em andamento", como pedido pelo desafio.
`cmd/api` usa o ciclo de vida nativo do Fx (`OnStop`) para o mesmo efeito
no `net/http.Server`.

## 13. Ambiente Docker

`docker compose -f deployments/docker-compose.yml up --build` sobe o
sistema inteiro numa passada: Postgres, Keycloak (`start-dev
--import-realm`) e LocalStack (`SERVICES=sqs`) primeiro, cada um até
ficar `healthy`; depois um serviço `migrate` (imagem oficial
`migrate/migrate`) aplica as migrations pendentes e **termina** (não fica
no ar); só então os 4 binários (construídos a partir do `Dockerfile`
multi-stage na raiz, mesmo build, `ARG BIN` escolhe qual binário vai para
cada imagem) sobem, coordenados por `depends_on` +
`service_completed_successfully`.

As filas SQS não são criadas pelo compose: `outbox-publisher` e
`wager-consumer` as criam (`CreateQueue`, idempotente) na primeira
execução contra o LocalStack.

## 14. Limitações conhecidas, interpretações e trabalho não concluído

Estado em 25/09/2026, na reta final antes da entrega. Lista condensada —
detalhe completo, com justificativa de cada item, em `docs/PROJETO.md`,
seção 5:

- **Sem garantia de ordem por agregado sob falha na publicação da
  outbox**: um evento mais recente do mesmo `aggregateId` pode ser
  publicado antes de um evento anterior que está em backoff — o
  `MessageGroupId = aggregateId` já usado no envio (`sqs_event_publisher.go`)
  só ordena o que já foi enviado, não evita que o `Claim` selecione o
  evento novo primeiro. Avaliado contra a seção 11 do desafio (não
  exige ordem garantida); decisão consciente de não corrigir, dado o
  prazo — melhoria futura seria o `Claim` respeitar a ordem de
  ocorrência por agregado.
- **Worker de referências pendentes** validado só por testes unitários
  com fakes em memória; ainda não executado contra Postgres real (nem
  teste de integração, nem execução manual) — única exceção entre os 4
  binários, que têm validação ponta a ponta contra infraestrutura real.
- **Sem métricas** (contadores por status, duplicatas, retries, DLQ,
  atraso da outbox, latência, divergências de reconciliação — seção 12
  do desafio). Tracing com OpenTelemetry e dashboards seguem como
  diferencial opcional, não feitos.
- **Sem testes formais de recuperação de falha e multi-instância**
  (≥ 3 processos) além do que os testes de integração já exercitam
  indiretamente via `SKIP LOCKED`.
- O teste de integração "negativo" da seção 13 (remover de propósito o
  `FOR UPDATE`/a condição de saldo e confirmar que o teste correspondente
  falha) foi feito manualmente, não está automatizado.

**Já resolvido e validado** (registrado aqui porque versões anteriores
deste documento ainda listavam estes itens como pendentes):
`docker compose up --build` completo, testes automatizados de handler
HTTP (`wallet_handler_test.go`/`wager_handler_test.go`/`errors_test.go`),
teste de integração automatizado contra o Keycloak real
(`keycloak_auth_integration_test.go`), observabilidade básica (logs
estruturados em JSON + `GET /health/ready` cobrindo Postgres, SQS e
Keycloak) e a verificação da composição Fx/ciclo de vida exigida pela
seção 13 (`fx_lifecycle_integration_test.go`, seção 11 acima).

## 15. Como reproduzir a solução do zero

Ver [`README.md`](./README.md), seção **⚙️ Como rodar**, para o passo a
passo completo (pré-requisitos, `.env`, `docker compose up --build`,
migrations, comandos de teste, exemplos de chamada autenticada).
