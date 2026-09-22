<div align="center">

# 🤝 Contribuindo — WagerFlow

*Projeto pessoal (desafio técnico), com um padrão de commits simples para manter o histórico de desenvolvimento legível.*

</div>

---

## Padrão de commits

Formato: `tipo(escopo opcional): descrição no imperativo`

| Tipo | Quando usar |
|---|---|
| `feat` | nova funcionalidade |
| `fix` | correção de bug |
| `docs` | documentação |
| `refactor` | mudança de código sem alterar comportamento |
| `test` | adição ou ajuste de testes |
| `chore` | configuração, dependências, tooling |

Quando fizer sentido, adiciono um escopo entre parênteses: `feat(wallet): ...`, `feat(idempotency): ...`.

### Exemplos

```
docs: registra contexto do desafio e decisões de arquitetura
feat(domain): implementa Money e Wallet com invariantes de saldo
feat(http): adiciona endpoint de abertura de carteira
feat(sqs): implementa consumidor com inbox transacional
fix(ledger): corrige cálculo de balanceAfter em reversões
test(concurrency): adiciona teste de disputa entre duas apostas
chore: configura Docker Compose com Postgres, LocalStack e Keycloak
```

> **Regra de ouro:** um commit, uma mudança. Descrição sempre no imperativo ("adiciona", não "adicionado").

---

## Checklist antes de commitar

- [ ] O código compila e roda localmente (`go run ./cmd/api`)
- [ ] `go vet ./...` passa sem erros
- [ ] `go test ./...` e `go test -race ./...` passam
- [ ] Código formatado com `gofmt`
- [ ] A mensagem de commit segue o padrão `tipo(escopo): descrição`
- [ ] Mudanças de schema vieram acompanhadas de migration versionada
- [ ] Nenhuma variável sensível (chaves, segredos, credenciais) foi commitada

---

## Organização de branches

Como é um projeto solo, o fluxo é simples:

- `main` — sempre em estado funcional
- `feature/<nome>` — para blocos de trabalho maiores, quando vale isolar antes de mergear

---

Dúvidas sobre decisões de arquitetura ou trade-offs específicos: consulte [`docs/PROJETO.md`](./docs/PROJETO.md).
