# Documentação de Decisões — Backend de Apostas (Jungle Gaming)

> Este documento registra o processo de decisão do desafio técnico da Jungle Gaming (Backend Developer Júnior — Go), não apenas o resultado final. O objetivo é demonstrar profissionalismo e raciocínio, especialmente por se tratar de um desenvolvedor júnior aprendendo Go durante o próprio desafio.

## Índice

1. [O desafio original](#1-o-desafio-original)
2. [Escopo e priorização](#2-escopo-e-priorização)
3. [Decisões de arquitetura](#3-decisões-de-arquitetura)
4. [Decisões de domínio e dados](#4-decisões-de-domínio-e-dados)
5. [Limitações conhecidas e trabalho não concluído](#5-limitações-conhecidas-e-trabalho-não-concluído)
6. [Fluxo de desenvolvimento e plano dia a dia](#6-fluxo-de-desenvolvimento-e-plano-dia-a-dia)

---

## 1. O desafio original

Ver `docs/DESAFIO.md` para o enunciado completo. Resumo: serviço em Go para processar operações financeiras de apostas (BET, WIN, LOSS, REFUND, ROLLBACK), com garantias de idempotência persistente, concorrência segura por carteira, ledger auditável, mensageria assíncrona (SQS) e autenticação via IdP externo (Keycloak).

**Prazo**: 3 dias corridos, a partir de 22/09/2026.

## 2. Escopo e priorização

Dado o prazo curto e a falta de experiência prévia em Go, a estratégia de priorização segue os critérios eliminatórios do desafio (não o volume de features). A tabela abaixo é atualizada conforme o projeto avança.

| Prioridade | Item | Motivo |
|---|---|---|
| P0 (eliminatório) | Autenticação real via Keycloak em todos os endpoints de negócio | Ausência disso reprova o candidato |
| P0 (eliminatório) | `Money` sem `float32`/`float64` | Ausência disso reprova o candidato |
| P0 (eliminatório) | Idempotência persistida no banco (não em memória) | Ausência disso reprova o candidato |
| P0 (eliminatório) | Sem saldo negativo / sem movimentação duplicada sob concorrência | Ausência disso reprova o candidato |
| P0 (eliminatório) | Evento publicado só após commit da transação de origem | Ausência disso reprova o candidato |
| P0 (eliminatório) | Ledger append-only auditável | Ausência disso reprova o candidato |
| P0 (eliminatório) | Testes de integração com Postgres/SQS/IdP reais (não 100% mock) | Ausência disso reprova o candidato |
| P1 | Fluxo HTTP completo (abertura de carteira, envio de operação, consulta, reconciliação) | Núcleo funcional do desafio |
| P1 | Consumidor SQS + inbox/outbox transacional | Parte central da nota (mensageria e recuperação = 15 pts) |
| P2 | Referências pendentes (`PENDING_REFERENCE`) com worker de retry | Cenário avançado, mas pontuado |
| P3 | Observabilidade completa (métricas, tracing) | Diferencial declarado como opcional |
| P3 | Testes de carga | Diferencial declarado como opcional, sem meta de RPS |

## 3. Decisões de arquitetura

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Controle de concorrência na carteira | Update atômico condicionado (`UPDATE ... WHERE balance >= X`) | Evita lock global e locks explícitos no código; o próprio SQL garante a invariante de saldo em uma única ida ao banco, sem necessidade de retry loop. Mais simples de implementar e testar sob prazo curto do que lock pessimista ou otimista com versionamento. |
| Router HTTP | `chi` | Só complementa o `net/http` (handlers continuam sendo `http.Handler` padrão), é leve e fácil de aprender para quem está aprendendo Go. `gin` traria convenções próprias fora do `net/http`; `http.ServeMux` puro foi a alternativa considerada, mas o `chi` dá subrotas e middlewares (`Recoverer`, `RequestID`) prontos. |
| Composição da aplicação | Uber Fx, restrito a `cmd/api/main.go` | Exigido pelo desafio. Só a camada de composição conhece o Fx; `domain`, `application` e `infrastructure` continuam sem depender dele. |
| Validação de tokens (Keycloak) | `coreos/go-oidc` + `golang-jwt/jwt` | Padrão de mercado para validar contra um IdP OIDC real (descoberta, JWKS, assinatura, expiração). Validação manual daria mais controle, mas mais código e mais risco de erro de segurança. Ver seção 4.7 para os detalhes de implementação. |

## 4. Decisões de domínio e dados

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Representação de `Money` | `int64` em centavos (menor unidade monetária) | Evita totalmente `float32`/`float64` (requisito eliminatório). Mais simples de implementar do que integrar uma lib de precisão decimal, e suficiente para o escopo do desafio (moeda única, BRL). |
| Máquina de estados de `WagerTransaction` | Métodos de transição (`MarkProcessed`, `MarkPendingReference`, `MarkRejected`, `MarkFailed`) com guard de estado terminal | Nenhuma transação em `PROCESSED`/`REJECTED`/`FAILED` pode mudar de estado de novo — isso é o que garante que um replay/reentrega apenas consulte o resultado já persistido, em vez de reaplicar a operação. |
| Separação entre `OPENING` (interno) e tipos externos | `NewOpeningTransaction` (uso interno) vs. `NewExternalWagerTransaction` (rejeita `OPENING` explicitamente) | O desafio exige rejeitar `OPENING` vindo de HTTP/SQS. Construtores separados tornam essa regra impossível de esquecer, em vez de uma checagem solta em algum handler. |
| Validação de `WalletLedgerEntry` no construtor | `NewWalletLedgerEntry` recalcula `balanceBefore ± amount` e compara com `balanceAfter` recebido, rejeitando se não bater | Constrói a garantia de correção matemática do ledger dentro do próprio domínio, não depende de quem chama ter calculado certo. |
| Driver de acesso ao Postgres | `pgx/v5` (com `pgxpool`), SQL explícito nos repositórios | Preferencial segundo o desafio; dá controle total sobre as queries do `UPDATE` atômico condicionado, sem camada de abstração escondendo o SQL. |
| Padrão de transação multi-repositório | `UnitOfWork` + `TxRunner` (interfaces no domínio), implementados por `TxManager` no Postgres | Permite que `Wallet`, `WagerTransaction` e `WalletLedgerEntry` sejam gravados no mesmo commit sem a camada `application` depender diretamente de Postgres — mantém a regra "domínio independente de framework/infra" do desafio. |
| Repositórios recebem `DBTX` (interface), não `*pgxpool.Pool` | `DBTX` é satisfeita tanto por `*pgxpool.Pool` quanto por `pgx.Tx` | O mesmo código de repositório funciona rodando solto ou dentro de uma transação — sem duplicar lógica de acesso a dados. |
| Saldo resultante persistido na transação | Coluna `resulting_balance_cents` em `wager_transactions`, preenchida em `MarkProcessed(saldo)` | A seção 9 do desafio exige que o replay devolva o saldo observado no processamento original, mesmo que a carteira já tenha outras movimentações. Sem guardar o saldo, o replay só teria o saldo atual. |
| Referência interna resolvida persistida | Coluna `reference_transaction_id` (FK) + `ResolveReference` no domínio | A seção 6.3 pede a referência interna resolvida. Permite auditar exatamente qual operação uma reversão desfez. |

### 4.1. Idempotência, corridas e reversões

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Hash do payload | JSON canônico (chaves ordenadas, via `json.Marshal` de `map[string]string`) + SHA-256 hex, calculado na camada `application` (`payload_hash.go`) | Mesmo conteúdo gera sempre o mesmo hash, e HTTP e SQS produzem o mesmo hash porque ambos montam o mesmo `ProcessWagerCommand`. |
| Campos do hash | providerId, externalTransactionId, playerId, walletId, roundId, gameId, kind, amount, currency, referenceExternalTransactionId. **Fora:** chave de idempotência e metadados de transporte | O desafio manda excluir a chave e o transporte. |
| Normalizações antes do hash | Dinheiro pelo `Money.String()` do valor já parseado; moeda em maiúsculas; UUIDs em forma canônica. IDs textuais (provedor, id externo, rodada, jogo) **não** são normalizados | Uma única forma textual por valor, sem alterar identificadores dos provedores. |
| Fluxo de idempotência | Na mesma transação SQL: busca por (provedor, chave) → mesmo hash = replay; hash diferente = conflito. Se a chave é nova mas (provedor, id externo) já existe = conflito | Cobre as três regras da seção 9. Todas as buscas incluem o `providerId` (isolamento entre provedores). |
| Corrida entre requisições iguais | O índice único do banco decide o vencedor. O perdedor recebe `ErrDuplicateTransaction` (traduzido do SQLSTATE 23505, só para os dois índices de idempotência), desfaz a tentativa e **relê o vencedor numa transação nova** | Depois de um erro de INSERT o Postgres marca a transação como abortada, então a releitura não pode acontecer na mesma transação. |
| Rejeição de negócio | É gravada e commitada como `REJECTED` com `failureCode`, não é rollback | Assim a rejeição é auditável e o replay devolve a mesma rejeição em vez de reaplicar. O `UPDATE` condicionado que afeta 0 linhas não aborta a transação. |
| Entradas inválidas | Não viram `WagerTransaction` (valor mal formatado, carteira inexistente, jogador que não é o dono, moeda diferente da carteira). São erros de validação sem efeito persistido | São corrigíveis e sem efeito; `wallet_id` é FK obrigatória, então nem há como gravar para carteira inexistente. |
| Concorrência entre reversões | `SELECT ... FOR UPDATE` na linha da referência + índice único parcial `uq_wager_single_processed_reversal` como rede de segurança do banco | Duas reversões da mesma aposta ficam em fila por referência (sem lock global); a segunda já enxerga a primeira. |
| Política de REFUND/ROLLBACK | Uma referência aceita no máximo **uma** reversão bem-sucedida, de qualquer tipo. ROLLBACK depois de REFUND da mesma aposta é rejeitado. Desfazer o próprio REFUND (ROLLBACK apontando para o REFUND) é permitido | Impede devolver duas vezes o mesmo débito (seção 7). Interpretação minha, o desafio deixa a combinação em aberto. |
| Direção do ROLLBACK | Sobre BET: crédito. Sobre WIN ou REFUND: débito | "Movimento contrário ao original" (seção 7). |
| Referência indisponível | Não existe, ou existe mas ainda está `PENDING`/`PENDING_REFERENCE` → `PENDING_REFERENCE`. Existe e terminou `REJECTED`/`FAILED` → `REJECTED` com `REFERENCE_NOT_PROCESSED` | Esperar só faz sentido se a referência ainda pode virar `PROCESSED`. |
| Reaproveitamento pelo worker | A regra de aplicação (`applyWagerTransaction`) é separada do caso de uso e não abre transação | O worker de referências pendentes vai reaplicar uma transação sem duplicar nenhuma regra. |

### 4.2. Máquina de estados de `WagerTransaction`

```
PENDING ──► PROCESSED            (terminal)
   │  └───► REJECTED             (terminal, exige failureCode)
   │  └───► FAILED               (terminal, exige failureCode)
   └──────► PENDING_REFERENCE ──► PROCESSED | REJECTED | FAILED
```

- `PENDING_REFERENCE` **não** é terminal: o worker precisa conseguir concluir a transação depois.
- Terminais não aceitam nenhuma transição. É isso que faz o replay só consultar o resultado já gravado.
- **Rejeição (`REJECTED`)** = resultado de regra de negócio, definitivo para aquela operação. **Falha (`FAILED`)** = falha permanente de infraestrutura registrada para auditoria.
- **Falha transitória** (banco indisponível, timeout) **não** vira estado: o erro sobe, a transação SQL é desfeita e o cliente pode tentar de novo com a mesma chave, sem efeito duplicado. _(Critério para promover uma falha a `FAILED` ainda a definir junto ao consumidor SQS/DLQ.)_

### 4.3. Códigos de falha (`failureCode`)

Todos são definitivos para a operação: fica `REJECTED` e um replay devolve a mesma rejeição. Para tentar de novo, o provedor envia uma operação nova (outro id externo e outra chave).

| Código | Quando |
|---|---|
| `INSUFFICIENT_BALANCE` | BET sem saldo suficiente |
| `REVERSAL_INSUFFICIENT_BALANCE` | Reversão que precisaria debitar mais que o saldo (ex: ROLLBACK de um WIN já gasto). Diferente do anterior, como a seção 7 exige |
| `REFERENCE_NOT_FOUND` | Referência não apareceu até esgotar tentativas/TTL _(a ser usado pelo worker)_ |
| `REFERENCE_NOT_PROCESSED` | Referência existe, mas terminou `REJECTED`/`FAILED` |
| `REFERENCE_MISMATCH` | Operação e referência discordam em jogador, carteira, moeda ou rodada |
| `REFERENCE_INVALID_KIND` | Combinação de tipos não permitida (WIN/REFUND só apontam para BET; ROLLBACK para BET, WIN ou REFUND) |
| `REFERENCE_ALREADY_REVERSED` | A referência já recebeu uma reversão bem-sucedida |
| `REVERSAL_AMOUNT_MISMATCH` | Valor da reversão diferente do valor da referência (sem reversão parcial) |

Entradas **corrigíveis** (não geram registro): valor inválido, `OPENING` vindo de fora, `REFUND`/`ROLLBACK` sem referência, carteira inexistente, jogador que não é o dono, moeda diferente da carteira.

### 4.4. Outbox transacional (escrita dos eventos)

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Onde o evento é gravado | Dentro da MESMA transação SQL da mudança de estado, via `UnitOfWork.Outbox()` (`OutboxRepository.Append`) | Nunca existe evento sem o fato que o originou, nem fato confirmado sem evento. Nada é publicado antes do commit, porque quem publica é um worker separado, que só enxerga linhas já confirmadas. |
| Onde a regra "qual evento sai" mora | No ponto onde o estado final é gravado: `applyMovement`, `applyLoss`, `reject` e `markPendingReference` (`wager_applier.go`), e no `OpenWalletUseCase` | Como `applyWagerTransaction` é reutilizado pelo worker de referências pendentes, o worker herda a emissão dos eventos sem duplicar regra. |
| Tipo e versão do evento | Definidos pelo construtor de cada evento (`domain/outbox_event.go`); versão 1 | O chamador não escolhe tipo nem versão; uma mudança incompatível de contrato vira versão 2 no construtor. |
| Construtores validam o estado | `WagerTransactionProcessed` exige transação `PROCESSED` com saldo resultante; `Rejected` exige `REJECTED`; `PendingReference` exige `PENDING_REFERENCE` com referência externa | O evento nunca descreve algo que ainda não aconteceu. |
| Payload | Envelope completo (`eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId` opcional, `occurredAt` UTC RFC 3339 com ms, `version`, `data`) serializado no momento da criação e guardado como JSONB | Snapshot imutável: alterar a transação depois não muda o que foi registrado. Não exigiu migration nova. Dinheiro sai sempre como string decimal. |
| `aggregateId` | Id da transação nos eventos de transação; id da carteira em `WalletBalanceChanged` | Interpretação minha (o desafio não fixa). Eventos de uma mesma carteira ficam agrupáveis por `aggregateId`. |
| `correlationId` | Vem do `context.Context` (`application.WithCorrelationID`); se a entrada não informar, usa o id da transação | É dado de rastreamento, não de negócio: não entra no hash de idempotência e será reaproveitado nos logs. Evita mudar a assinatura de cada função. |
| `causationId` | Só em `WalletBalanceChanged`, apontando o `eventId` do `WagerTransactionProcessed` que o causou | Opcional no contrato; deixa a relação causa/efeito explícita. |
| `WalletBalanceChanged` | Construído a partir do lançamento do ledger + versão da carteira devolvida pelo `UPDATE ... RETURNING` | Evento e ledger não têm como divergir (o lançamento já foi validado: `balanceAfter = balanceBefore ± valor`). |
| Abertura de carteira | Saldo inicial positivo grava `WagerTransactionProcessed` (kind `OPENING`, sem metadados externos) e `WalletBalanceChanged` (versão 1) no commit da carteira. Saldo zero não grava eventos | Seção 9 do desafio. |
| Quais eventos por resultado | BET/WIN/REFUND/ROLLBACK processados: `Processed` + `BalanceChanged`. LOSS: só `Processed`. Rejeição: `Rejected`. Referência ausente: `PendingReference` | Seções 7 e 11 do desafio. |
| `PendingReference` só na 1ª vez | Emitido apenas quando a transação sai de `PENDING` para `PENDING_REFERENCE` | Quando o worker reaplicar e a referência continuar ausente, não publica o mesmo aviso a cada tentativa. |
| Replay | Não grava eventos | O replay só consulta o resultado persistido; não reaplica a operação. |

### 4.5. Testes de integração e correções encontradas por eles

Escritos em `test/integration/` (build tag `integration`), cada teste roda contra um Postgres **real**, num banco criado e apagado na hora (não um schema — um banco novo por teste, com as migrations aplicadas do zero). É por isso que existem numa pasta separada, com build tag: não rodam em `go test ./...` normal (que não pode depender de infraestrutura de fora) nem em CI sem um Postgres disponível.

Cobrem os cenários obrigatórios da seção 13: migrations up/down/up; imutabilidade do ledger (`UPDATE`, `DELETE` e `TRUNCATE`); as duas apostas de 80.00 sobre saldo de 100.00 (30 rodadas, para não passar por sorte); a mesma aposta 50× em paralelo (só 1 débito); carteiras diferentes em paralelo (sem lock global); duas reversões concorrentes da mesma aposta (só 1 vence); idempotência sobrevivendo a um "reinício" (pool novo, mesmo banco); atomicidade com erro no meio da transação e com `panic`; atomicidade da outbox (evento no mesmo commit do estado, replay não duplica).

Escrever estes testes encontrou 3 bugs reais, todos corrigidos:

1. **`findExisting` sob corrida (`application/process_wager_transaction.go`)** — a checagem de idempotência faz duas buscas separadas: por `idempotencyKey` e por `(providerId, externalTransactionId)`. Em `READ COMMITTED` (o nível padrão do Postgres), cada `SELECT` dentro da mesma transação pode enxergar um instante diferente do banco. Sob corrida real, era possível a segunda busca (por `externalId`) já enxergar a linha que o vencedor acabou de inserir, enquanto a primeira busca (por `idempotencyKey`) — que rodou um instante antes — ainda não a via. O código tratava isso como `ErrExternalTransactionConflict` (um `externalId` "roubado" por outra chave), quando na verdade era a MESMA operação, só vista em outro instante. Corrigido: quando o registro achado por `externalId` tem a mesma `idempotencyKey` da candidata, é tratado como a mesma operação (replay), não como conflito. Reproduzido em teste unitário com um repositório "desalinhado" de propósito (`internal/application/find_existing_test.go`), sem precisar de Postgres para provar a lógica.
2. **`TRUNCATE` na ledger não era bloqueado** — a migration 000003 criou triggers `BEFORE UPDATE`/`BEFORE DELETE`, do tipo `FOR EACH ROW`. `TRUNCATE` é um comando de outra categoria (nível de comando, não de linha): nenhum dos dois triggers dispara, e a tabela append-only podia ser esvaziada sem erro nenhum. Corrigido pela migration `000006`, um terceiro trigger `BEFORE TRUNCATE ... FOR EACH STATEMENT`, reaproveitando a mesma função `prevent_ledger_mutation()`.
3. **`Money`: `"-0.50"` virava `+0.50`; `"25.+5"` virava `25.05`** — a validação antiga rejeitava negativo checando se a parte inteira (`wholePart`) era `< 0`; para `"-0.50"`, essa parte é `"-0"`, que o `ParseInt` lê como `0` — nem positivo nem negativo — então o sinal era perdido silenciosamente. Da mesma forma, a parte decimal era passada direto para `ParseInt`, que aceita um `"+"` de propósito (`"+5"` é um `int64` válido), então `"25.+5"` virava `25.05` em vez de ser rejeitado. Corrigido tratando o sinal separadamente ANTES de dividir em parte inteira/decimal, e exigindo que as duas partes sejam só dígitos. Também foram adicionados `Money.Negate()` (exigido pela seção 6.1) e checagem de overflow em `Add`/`Subtract` (havia um `TODO` no código antigo).

### 4.6. API HTTP

Implementada em `internal/interfaces/http/` (router chi, handlers, DTOs, mapeamento de erros) e composta com Uber Fx em `cmd/api/main.go`. Rotas e tabela de status também estão resumidas no README.

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Handlers finos | Handler só converte HTTP ↔ caso de uso (JSON, UUID, header) e devolve o status; regra de negócio fica em `domain`/`application` | Mantém o isolamento de framework exigido pelo desafio: trocar chi por outra coisa não toca em regra alguma. |
| Dinheiro no JSON | Sempre string decimal (`"25.00"`) num objeto `{amount, currency}` | Um `number` JSON vira `float` na maioria dos clientes; a seção 6.1 proíbe float em qualquer ponto do contrato. |
| `Idempotency-Key` | Header obrigatório em `POST /wagering/transactions`; o servidor **nunca** o substitui por um valor calculado | Seção 9 do desafio. Header ausente = `400`. |
| Rejeição de negócio não é erro HTTP | `REJECTED` volta como `200` com o resultado; replay também é `200` | A operação foi aceita e avaliada; a recusa é o resultado (e o replay devolve o mesmo resultado, de forma estável). |
| Distinguibilidade do contrato | `201` processada · `202` aguardando referência · `200` replay/rejeitada · `400` entrada inválida · `404` não encontrado · `409` conflito · `422` regra de negócio · `503` falha transitória | A seção 9 exige que cada situação seja distinguível pelo contrato. Corpo de erro padrão `{code, message}` com `code` estável para o cliente decidir programaticamente. |
| Erros não classificados | Viram `503` com mensagem genérica | Assume-se falha transitória: a transação SQL foi desfeita, então repetir com a mesma chave é seguro. A mensagem interna não vaza para o cliente. |
| Conflitos | Chave de idempotência com payload diferente, id externo usado por outra chave, carteira duplicada (`uq_wallets_player_currency` → `ErrWalletAlreadyExists`) e `ErrDuplicateTransaction` que escape → `409` | Mapeados por `errors.Is` sobre erros do domínio, sem o handler conhecer o banco. |
| Correlation id | Middleware lê/gera `X-Correlation-Id`, devolve no header e coloca no `context` via `application.WithCorrelationID` | O mesmo id vai para os eventos da outbox (seção 4.4); reaproveitável nos logs. |
| Paginação do ledger | Cursor opaco (base64 de `createdAt\|id`), ordem estável `(createdAt, id)` crescente, pede `limit+1` linhas para saber se há próxima página | Sem `COUNT` extra e sem expor o formato do cursor ao cliente. |
| Reconciliação | `POST /wallets/{id}/reconciliation` lê a carteira e soma o ledger na **mesma transação** (`SumByWallet`), sem alterar nada | Compara saldo armazenado × saldo reconstruído numa visão consistente dos dados (seção 9). |
| Health | `live` = processo vivo (sem dependências); `ready` = Postgres respondendo (`Ping`) | Um banco fora do ar não deve fazer o orquestrador reiniciar a aplicação à toa. SQS entra em `ready` quando existir. |
| Configuração | `internal/config` é o único lugar que conhece os nomes das variáveis (`DATABASE_URL` obrigatória, `HTTP_PORT` padrão `8080`); `.env` carregado só em `main` | Nenhum `os.Getenv` espalhado; `config` testável sem arquivo em disco. |
| Encerramento | Fx fecha o pool no `OnStop`; o servidor HTTP faz shutdown gracioso (10 s) | Requisições em andamento terminam antes de as conexões serem derrubadas. |

**Consultas de repositório adicionadas para a API**: `WagerTransactionRepository.FindByID`, `WalletLedgerEntryRepository.ListByWallet` e `SumByWallet`, `DBTX.Query` (necessário para listagem multi-linha). Os fakes de teste em memória ganharam os mesmos métodos.

### 4.7. Autenticação e autorização (Keycloak/OIDC)

Requisito eliminatório (seção 14 do desafio). Implementado em `internal/interfaces/http/auth_middleware.go`, composto no Fx (`cmd/api/main.go`) e provisionado no `docker compose` (`deployments/keycloak/`).

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Provisionamento do Keycloak | `start-dev --import-realm`, com `deployments/keycloak/realm-export.json` montado como volume | Sobe pronto, sem passo manual pela UI do Keycloak — importante porque o candidato não tem muita familiaridade com a ferramenta e o ambiente precisa ser reproduzível com `docker compose up -d`. |
| Fluxo OAuth2 | `client_credentials`, um client por identidade (`provider-a`, `provider-b`, `wagerflow-internal`) | Comunicação serviço-a-serviço, recomendado pelo próprio desafio (seção 2); sem senha de usuário nem emissão própria de token, que estão fora do escopo. |
| Como o `providerId` é resolvido a partir do token | Claim `azp` (authorized party) do access token — que já é, por padrão, o `client_id` de quem pediu o token no Keycloak | Evita precisar de um protocol mapper customizado no realm: cada provedor JÁ tem um client próprio, então `client_id = providerId` é suficiente. Decisão registrada como interpretação minha — o desafio não define de qual claim tirar essa identidade. |
| Autorização | Dois realm roles: `provider` (rotas de wagering) e `internal` (rotas de wagering **e** de carteira) | A seção "Autenticação e autorização" do desafio exige "restrição das operações internas" — carteira é operação interna, então só `internal` entra lá. `internal` também pode tudo de `provider`, sem checagem de `providerId` (é o próprio serviço). |
| Verificação do token | `go-oidc` faz o *discovery* OIDC (busca `jwks_uri` automaticamente) e confere assinatura/issuer/expiração via `provider.Verifier(&oidc.Config{SkipClientIDCheck: true})`; `golang-jwt` só decodifica as claims customizadas (`azp`, `realm_access.roles`) num tipo forte, depois que o `go-oidc` já validou o token | `SkipClientIDCheck: true` porque o `go-oidc` foi pensado para ID tokens (que têm `aud` = client_id); em `client_credentials` o access token não tem essa garantia. A checagem de identidade é feita por nós, via `azp` + role, não pela checagem de `aud` da lib. |
| Isolamento entre provedores | Comparação explícita, dentro de cada handler de wagering, entre o `providerId` do token e o da requisição (corpo em `POST`, URL em `GET /providers/...`) | O desafio exige isolamento "inclusive em consultas e replays" — não dá para confiar no `providerId` que o cliente manda, mesmo autenticado, porque nada impede um `provider-a` autenticado de mandar `providerId: "provider-b"` no corpo. |
| Resposta de descasamento por URL/corpo | `403 PROVIDER_MISMATCH` | O provider já está afirmando explicitamente "quero o recurso do provider X" — não há nada a esconder sobre existência, então 403 é honesto. |
| Resposta de descasamento por id interno (`GET /wagering/transactions/:id`) | `404` (igual a "não existe"), não `403` | Aqui a URL não menciona `providerId` nenhum; devolver 403 confirmaria para um provider que aquele id EXISTE e pertence a outro alguém — a seção "Autenticação e autorização" do desafio proíbe "exposição de dados em acessos não autorizados". `404` é indistinguível de um id que nunca existiu. |
| Health checks | Continuam sem autenticação | Health check é chamado por orquestrador/monitoramento, não por um cliente autenticado; a seção 9 não lista auth como requisito dos health checks. |
| Testes automatizados | `auth_middleware_test.go` cobre `AuthMiddleware` e `RequireRole` com um `TokenVerifier` fake (sem Keycloak real) — 401 sem header, 401 com token inválido/expirado, 403 sem role, propagação correta de `providerId`/roles no context | A lógica de decisão (extrair Bearer, decidir 401/403, propagar identidade) é testável isoladamente da infraestrutura; validar contra o Keycloak real de verdade foi feito manualmente (ver limitação abaixo). |

**Verificado manualmente** (sem Keycloak/HTTP mockado, contra os containers reais): health público sem token (200); rota protegida sem token (401 `MISSING_CREDENTIALS`); token de `provider-a` numa rota que exige `internal` (403 `FORBIDDEN`); `provider-a` consultando `providers/provider-b/...` (403 `PROVIDER_MISMATCH`); token `internal` entrando numa rota de carteira (chega até a regra de negócio, não é barrado pela auth).

## 5. Limitações conhecidas e trabalho não concluído

> Esta seção deve ser mantida honesta e atualizada até a entrega final — é parte da nota de documentação (5 pts) e demonstra maturidade profissional, mesmo quando o item não foi feito por falta de tempo.

Estado em 24/09/2026 (dia 3 de 3). Itens abaixo ainda **não** existem (ou estão incompletos) e serão marcados como concluídos ou como limitação na entrega:

- **A camada HTTP não tem testes automatizados de handler.** A auth tem teste próprio (`auth_middleware_test.go`, seção 4.7), mas os handlers de wallet/wager (formato do JSON, mapeamento de status, isolamento de `providerId` dentro do handler, formato do cursor) ainda são conferidos só manualmente (`curl`) e indiretamente pelos testes de `application`/domínio.
- **Sem teste de integração automatizado contra o Keycloak real.** A validação do fluxo OIDC completo (discovery, JWKS, `client_credentials`, roles, isolamento entre provedores) foi confirmada manualmente com `curl` contra os containers reais (seção 4.7), não em `go test`. A seção 13 do desafio pede "Execute PostgreSQL, o IdP e LocalStack... em containers reais" nos testes de integração — falta migrar essa verificação manual para dentro de `test/integration/`.
- Sem `Dockerfile` e sem a API dentro do `docker compose`: hoje o compose sobe só o Postgres, a API roda com `go run ./cmd/api` e as migrations são aplicadas manualmente. O critério `docker compose up --build` completo fica para a etapa final.
- **Publicação da outbox (`cmd/outbox-publisher`) e consumidor SQS + inbox (`cmd/wager-consumer`) estão implementados e VALIDADOS de ponta a ponta contra infraestrutura real** (sessão 012/013): testes de integração automatizados rodando `ok` via Docker contra Postgres real (`wagerflow_integration_test.go` e `wager_consumer_integration_test.go`, 13 cenários no total), e teste manual ponta a ponta confirmado contra LocalStack real — carteira criada via API gerou evento em `wagerflow-events.fifo` (publisher), e uma mensagem manual em `wager-transactions.fifo` foi processada corretamente pelo consumidor (débito aplicado no Postgres, saldo e `version` corretos).
- **Limitação de design conhecida e aceita (outbox publisher): sem garantia de ordem por agregado sob falha.** O `Claim` do publisher (`SELECT ... FOR UPDATE SKIP LOCKED`) não impede que um evento mais recente do MESMO `aggregateId` seja publicado enquanto um evento anterior do mesmo agregado está em backoff após falha transitória — nesse cenário específico, a ordem de entrega na fila pode inverter em relação à ordem de ocorrência. Avaliado contra a seção 11 do desafio: o texto não exige ordem garantida entre eventos (exige múltiplos publishers, disputa por registros, backoff, recuperação de trabalho abandonado e preservação do `eventId` na republicação — tudo isso está implementado e testado). Decisão consciente de não corrigir agora, dado o prazo, por não ser critério eliminatório; ficaria como próxima melhoria (ex.: publicar em ordem estrita por agregado, ou usar `MessageGroupId` = `aggregateId` na fila FIFO de saída para a própria fila serializar por grupo).
- Sem worker de referências pendentes (o caso de uso já grava `PENDING_REFERENCE` e a regra de reaplicação — `applyWagerTransaction` — já é reaproveitável, mas nada retoma essas transações; falta a migration com tentativas/próximo retry e o binário do worker).
- `GET /health/ready` verifica só o Postgres; a checagem do SQS entra junto com o worker de referências pendentes ou na etapa de observabilidade.
- Sem observabilidade (logs JSON, métricas).
- Os testes de integração (seção 4.5) cobrem os cenários obrigatórios da seção 13, mas não incluem ainda o teste "negativo" descrito nela — remover de propósito o `FOR UPDATE`/a condição de saldo no `WHERE` e confirmar que o teste correspondente falha. É uma checagem manual, não automatizada no CI.
- O `ARCHITECTURE.md` exigido na seção 15 do desafio ainda não existe; este `PROJETO.md` será a base dele.

## 5.1. Consumidor SQS e inbox (implementado em 24/09/2026)

| Decisão | Escolha | Por quê (resumo) |
|---|---|---|
| Canal de entrada SQS | `ConsumeWagerTransactionUseCase`, em `internal/application`, reaproveitando `buildCandidate`/`findExisting`/`applyWagerTransaction` do processamento HTTP | Seção 10 do desafio: HTTP e SQS compartilham o mesmo caso de uso. O hash de idempotência e todas as regras de negócio são idênticos nos dois canais por construção, não por convenção. |
| Onde a inbox é gravada | Dentro da MESMA transação SQL do efeito financeiro (`TryInsert` + checagem de idempotência + débito/crédito, tudo em um `WithinTransaction`) | Mesmo raciocínio da outbox (seção 4.4): nunca existe mensagem "vista" sem o efeito correspondente, nem o contrário. |
| Deduplicação por `TryInsert` | `INSERT ... ON CONFLICT ON CONSTRAINT uq_inbox_consumer_message DO NOTHING`, decide pelo `RowsAffected()` | Atômico no próprio Postgres — sem corrida entre "checar se existe" e "inserir" sob múltiplas instâncias do consumidor. |
| Duas camadas de deduplicação | Inbox (`consumerName` + `messageId` do SQS) pega reentregas da MESMA entrega lógica; idempotência do domínio (`idempotencyKey`/`externalTransactionId`) pega a MESMA operação chegando em mensagens diferentes | São problemas diferentes: o SQS garante at-least-once por mensagem, mas nada impede o provedor de mandar a mesma operação de negócio duas vezes, em mensagens novas. |
| Formato do corpo da mensagem | JSON espelhando `processWagerRequest` do HTTP + `idempotencyKey` (que no HTTP é header) | O canal SQS não tem um equivalente nativo de "header de aplicação" isolado do corpo; colocar a chave no JSON é a opção mais simples. |
| Classificação de erro (`IsPermanentWagerError`) | Mesma lista de erros de domínio classificados como permanentes no comentário de `ProcessWagerTransactionUseCase.Execute`, mais um tipo próprio (`errInvalidMessage`) para corpo de mensagem malformado | Erro permanente = reentregar não muda o resultado (dado inválido, conflito). Erro transitório = infraestrutura, deve voltar para a fila. |
| Fila principal + DLQ | `wager-transactions.fifo` com `RedrivePolicy` apontando para `wager-transactions-dlq.fifo` (`maxReceiveCount` configurável, default 5) | Erro permanente é mandado **explicitamente** para a DLQ (não espera o redrive automático — não faz sentido reentregar várias vezes algo que já sabemos que vai falhar de novo). Erro transitório só não apaga a mensagem: ela volta pela `VisibilityTimeout`, e o `maxReceiveCount` é a rede de segurança para o caso de uma falha transitória virar, na prática, permanente. |
| Corrida entre mensagens da mesma operação | Reaproveita `ErrDuplicateTransaction` + relê o vencedor numa transação nova (mesma lógica de `replayInTransaction` do caminho HTTP, chamado via instância temporária de `ProcessWagerTransactionUseCase` dentro do mesmo pacote) | Evita duplicar a lógica de "relê o vencedor depois do índice único decidir" — ela já existia e está testada no caminho HTTP. |

## 6. Fluxo de desenvolvimento e plano dia a dia

| Dia | Foco |
|---|---|
| Dia 1 | Fundamentos de Go necessários + decisões de arquitetura + domínio puro (Money, Wallet, WagerTransaction, LedgerEntry) com testes unitários |
| Dia 2 | Persistência (migrations, repositórios), API HTTP, idempotência, autenticação |
| Dia 3 | SQS (inbox/outbox), testes de concorrência/recuperação, documentação final, revisão de código limpo |

### Checklist de progresso (24/09/2026, noite do dia 2)

- [x] Domínio puro com testes unitários (`Money`, `Wallet`, `WagerTransaction`, `WalletLedgerEntry`)
- [x] Migrations 000001–000006 e repositórios Postgres (`pgx/v5`), `UnitOfWork`/`TxRunner`
- [x] Caso de uso central `BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK` com idempotência persistente
- [x] Abertura de carteira (`POST /wallets`) com lançamento de abertura no ledger
- [x] Outbox transacional — escrita dos eventos na mesma transação
- [x] Testes de integração contra Postgres real (seção 13 do desafio), com `-race`
- [x] API HTTP com chi + composição com Uber Fx (rotas, status HTTP, correlation id, health)
- [x] Consultas de leitura: carteira, ledger paginado, transação (por id e por provedor + id externo), reconciliação
- [x] **Autenticação Keycloak/OIDC** (eliminatório) — Keycloak provisionado no compose, middleware de validação, isolamento entre provedores, restrição das operações internas
- [ ] Testes automatizados da camada HTTP (handlers de wallet/wager) e de integração contra o Keycloak real
- [x] Worker publicador da outbox + destino dos eventos de saída (LocalStack) — validado ponta a ponta contra Postgres/LocalStack reais
- [x] Consumidor SQS + inbox (`wager-transactions.fifo` + DLQ) — validado ponta a ponta contra Postgres/LocalStack reais
- [ ] Worker de referências pendentes (backoff, TTL, `REFERENCE_NOT_FOUND`)
- [ ] Testes de recuperação de falha e multi-instância; observabilidade básica
- [ ] `Dockerfile`, `docker compose up --build` completo, `ARCHITECTURE.md`, revisão final da documentação

---

## Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos de Go, revisão de decisões técnicas, geração de código de apoio e documentação, devido à curva de aprendizado da linguagem sob prazo curto. Todas as decisões de arquitetura, escopo e priorização foram tomadas pelo candidato, registradas com justificativa própria acima — não apenas o resultado final, mas o porquê de cada escolha.
