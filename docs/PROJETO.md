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

## 5. Limitações conhecidas e trabalho não concluído

> Esta seção deve ser mantida honesta e atualizada até a entrega final — é parte da nota de documentação (5 pts) e demonstra maturidade profissional, mesmo quando o item não foi feito por falta de tempo.

_(a preencher conforme o prazo se esgota)_

## 6. Fluxo de desenvolvimento e plano dia a dia

| Dia | Foco |
|---|---|
| Dia 1 | Fundamentos de Go necessários + decisões de arquitetura + domínio puro (Money, Wallet, WagerTransaction, LedgerEntry) com testes unitários |
| Dia 2 | Persistência (migrations, repositórios), API HTTP, idempotência, autenticação |
| Dia 3 | SQS (inbox/outbox), testes de concorrência/recuperação, documentação final, revisão de código limpo |

---

## Uso de IA neste projeto

Este projeto foi desenvolvido com apoio de IA (Claude) para explicação de conceitos de Go, revisão de decisões técnicas, geração de código de apoio e documentação, devido à curva de aprendizado da linguagem sob prazo curto. Todas as decisões de arquitetura, escopo e priorização foram tomadas pelo candidato, registradas com justificativa própria acima — não apenas o resultado final, mas o porquê de cada escolha.