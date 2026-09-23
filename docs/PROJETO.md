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

## 5. Limitações conhecidas e trabalho não concluído

> Esta seção deve ser mantida honesta e atualizada até a entrega final — é parte da nota de documentação (5 pts) e demonstra maturidade profissional, mesmo quando o item não foi feito por falta de tempo.

Estado em 23/09/2026 (fim do dia 2 de 3). Itens abaixo ainda **não** existem e serão marcados como concluídos ou como limitação na entrega:

- Sem API HTTP, sem composição com Uber Fx e sem autenticação (Keycloak). Autenticação real é requisito eliminatório.
- Sem consumidor SQS, sem escrita em inbox e sem publicação da outbox. As tabelas existem (migration 000004), mas nada as usa.
- Os eventos de outbox (`WagerTransactionProcessed`, `WalletBalanceChanged` etc.) **ainda não são gravados** na mesma transação, nem na abertura de carteira nem no processamento de operações (seções 6.5, 9 e 11 do desafio).
- Sem worker de referências pendentes (o caso de uso já grava `PENDING_REFERENCE`, mas nada as retoma; faltam tentativas e próximo retry, que exigem migration).
- Testes contra Postgres real (concorrência de 100.00 com duas apostas de 80.00, 50 requisições iguais em paralelo, reversões concorrentes, imutabilidade do ledger) ainda não foram escritos. Hoje o caso de uso é coberto por testes unitários com repositórios em memória, que **não** provam concorrência nem atomicidade.
- Sem endpoints de leitura, reconciliação e health checks; faltam as consultas de repositório correspondentes (busca por id, ledger paginado).
- Sem observabilidade (logs JSON, métricas).
- `Money.Add` ainda não checa overflow (há um `TODO` no código); o parsing aceita `+25.00` e zeros à esquerda, que o hash normaliza para a mesma forma.
- Os testes do caso de uso rodam com um `TxRunner` em memória que não simula rollback nem isolamento.
- O `ARCHITECTURE.md` exigido na seção 15 do desafio ainda não existe; este `PROJETO.md` será a base dele.

## 6. Fluxo de desenvolvimento e plano dia a dia

| Dia | Foco |
|---|---|
| Dia 1 | Fundamentos de Go necessários + decisões de arquitetura + domínio puro (Money, Wallet, WagerTransaction, LedgerEntry) com testes unitários |
| Dia 2 | Persistência (migrations, repositórios), API HTTP, idempotência, autenticação |
| Dia 3 | SQS (inbox/outbox), testes de concorrência/recuperação, documentação final, revisão de código limpo |

---

## Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos de Go, revisão de decisões técnicas, geração de código de apoio e documentação, devido à curva de aprendizado da linguagem sob prazo curto. Todas as decisões de arquitetura, escopo e priorização foram tomadas pelo candidato, registradas com justificativa própria acima — não apenas o resultado final, mas o porquê de cada escolha.
