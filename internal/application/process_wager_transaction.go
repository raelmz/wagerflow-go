package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// ProcessWagerCommand é a entrada do caso de uso. É a MESMA para HTTP e
// SQS: cada canal converte o seu formato (corpo JSON + header, ou
// mensagem do envelope) neste struct. Assim as garantias de
// idempotência valem igualmente nos dois caminhos (seção 10 do desafio:
// "HTTP e SQS devem compartilhar o caso de uso").
//
// Amount e Currency chegam como STRING de propósito: o parsing para
// Money acontece dentro do caso de uso, passando pelas validações do
// domínio (sem float em nenhum ponto).
type ProcessWagerCommand struct {
	IdempotencyKey                 string
	ProviderID                     string
	ExternalTransactionID          string
	PlayerID                       uuid.UUID
	WalletID                       uuid.UUID
	RoundID                        string
	GameID                         string
	Kind                           domain.WagerKind
	Amount                         string
	Currency                       string
	ReferenceExternalTransactionID string
}

// ProcessWagerResult é o resultado devolvido ao chamador.
// Quem monta a resposta HTTP olha Transaction.Status() para escolher o
// código (PROCESSED, REJECTED ou PENDING_REFERENCE) e usa Replay para
// preencher "idempotentReplay".
type ProcessWagerResult struct {
	Transaction *domain.WagerTransaction
	Replay      bool
}

// ProcessWagerTransactionUseCase processa BET, WIN, LOSS, REFUND e
// ROLLBACK vindos de fora. Depende só de domain.TxRunner.
type ProcessWagerTransactionUseCase struct {
	txRunner domain.TxRunner
}

func NewProcessWagerTransactionUseCase(txRunner domain.TxRunner) *ProcessWagerTransactionUseCase {
	return &ProcessWagerTransactionUseCase{txRunner: txRunner}
}

// Execute processa uma operação de forma idempotente.
//
// Erros devolvidos (todos classificáveis com errors.Is):
//   - domain.ErrInvalidAmount / ErrNegativeAmount / ErrInvalidWagerData /
//     ErrMissingReference: entrada inválida (HTTP 400/422). Sem efeito.
//   - domain.ErrWalletNotFound, ErrPlayerWalletMismatch,
//     ErrWalletCurrencyMismatch: dados incoerentes com a carteira. Sem efeito.
//   - domain.ErrIdempotencyConflict, ErrExternalTransactionConflict:
//     conflito (HTTP 409). Sem efeito.
//   - qualquer outro erro: falha de infraestrutura, transitória (HTTP 503).
//
// Rejeição de negócio (ex: saldo insuficiente) e pendência de
// referência NÃO são erro: voltam em Result com o status correspondente.
func (uc *ProcessWagerTransactionUseCase) Execute(ctx context.Context, cmd ProcessWagerCommand) (*ProcessWagerResult, error) {
	// Valida a entrada e monta a transação candidata ANTES de tocar no
	// banco: entrada inválida nem chega a abrir transação SQL.
	candidate, err := buildCandidate(cmd)
	if err != nil {
		return nil, err
	}

	result, err := uc.processInTransaction(ctx, candidate)

	// --- Corrida entre requisições iguais ---
	// Duas requisições com a mesma chave podem passar juntas pela
	// checagem "já existe?" e tentar inserir ao mesmo tempo. O índice
	// ÚNICO do banco deixa só uma vencer; a outra recebe
	// ErrDuplicateTransaction.
	//
	// Por que releio numa transação NOVA, e não na mesma? Depois de um
	// erro de INSERT, o Postgres marca a transação inteira como
	// "abortada" e recusa qualquer comando seguinte. Então a tentativa
	// perdedora é desfeita e o registro vencedor é lido numa transação
	// limpa. Nesse ponto o vencedor já fez commit (o INSERT do perdedor
	// esperou por ele antes de falhar).
	if errors.Is(err, domain.ErrDuplicateTransaction) {
		return uc.replayInTransaction(ctx, candidate)
	}

	return result, err
}

// buildCandidate valida o comando e cria a WagerTransaction (ainda
// PENDING, ainda não persistida) com o hash do payload calculado.
func buildCandidate(cmd ProcessWagerCommand) (*domain.WagerTransaction, error) {
	if cmd.PlayerID == uuid.Nil || cmd.WalletID == uuid.Nil {
		return nil, fmt.Errorf("%w: playerId e walletId são obrigatórios", domain.ErrInvalidWagerData)
	}

	money, err := domain.NewMoneyFromString(cmd.Amount, cmd.Currency)
	if err != nil {
		return nil, err
	}

	hash, err := computePayloadHash(cmd, money)
	if err != nil {
		return nil, err
	}

	return domain.NewExternalWagerTransaction(
		cmd.ExternalTransactionID, cmd.ProviderID, cmd.IdempotencyKey, hash,
		cmd.WalletID, cmd.PlayerID, cmd.RoundID, cmd.GameID,
		cmd.Kind, money, cmd.ReferenceExternalTransactionID,
	)
}

// processInTransaction é o caminho principal: tudo numa única transação
// SQL — checagem de idempotência, criação, movimentação e ledger.
func (uc *ProcessWagerTransactionUseCase) processInTransaction(
	ctx context.Context,
	candidate *domain.WagerTransaction,
) (*ProcessWagerResult, error) {
	var result *ProcessWagerResult

	err := uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		// 1. Idempotência: essa operação já foi vista?
		existing, err := findExisting(ctx, uow, candidate)
		if err != nil {
			return err
		}
		if existing != nil {
			result = &ProcessWagerResult{Transaction: existing, Replay: true}
			return nil
		}

		// 2. A carteira existe e é coerente com a operação?
		// Isso vem DEPOIS da idempotência de propósito: um replay de
		// algo já aceito continua funcionando.
		wallet, err := uow.Wallets().FindByID(ctx, candidate.WalletID())
		if err != nil {
			return err
		}
		if wallet == nil {
			return domain.ErrWalletNotFound
		}
		if wallet.PlayerID() != candidate.PlayerID() {
			return domain.ErrPlayerWalletMismatch
		}
		if wallet.Currency() != candidate.Money().Currency() {
			return domain.ErrWalletCurrencyMismatch
		}

		// 3. Registra a transação. Se outra requisição igual venceu a
		// corrida, o índice único faz este INSERT falhar com
		// ErrDuplicateTransaction (tratado em Execute).
		if err := uow.WagerTransactions().Create(ctx, candidate); err != nil {
			return err
		}

		// 4. Aplica a operação (débito/crédito/ledger/reversão) e
		// persiste o estado final. Sem commit intermediário de aceite:
		// se qualquer passo falhar, nada é gravado.
		if err := applyWagerTransaction(ctx, uow, candidate); err != nil {
			return err
		}

		result = &ProcessWagerResult{Transaction: candidate, Replay: false}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// replayInTransaction relê o registro vencedor de uma corrida.
func (uc *ProcessWagerTransactionUseCase) replayInTransaction(
	ctx context.Context,
	candidate *domain.WagerTransaction,
) (*ProcessWagerResult, error) {
	var result *ProcessWagerResult

	err := uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		existing, err := findExisting(ctx, uow, candidate)
		if err != nil {
			return err
		}
		if existing == nil {
			// Não deveria acontecer: o índice único disse que existe.
			return errors.New("conflito de unicidade sem registro correspondente")
		}
		result = &ProcessWagerResult{Transaction: existing, Replay: true}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// findExisting implementa as regras de idempotência da seção 9:
//
//   - mesma chave + mesmo conteúdo (hash)   -> devolve o registro (replay);
//   - mesma chave + conteúdo diferente      -> ErrIdempotencyConflict;
//   - chave nova, mas (providerId, externalTransactionId) já usado
//     por OUTRA chave                        -> ErrExternalTransactionConflict;
//   - nada encontrado                        -> (nil, nil), pode processar.
//
// Todas as buscas incluem o providerId: um provedor nunca enxerga nem
// colide com registros de outro.
func findExisting(ctx context.Context, uow domain.UnitOfWork, candidate *domain.WagerTransaction) (*domain.WagerTransaction, error) {
	repo := uow.WagerTransactions()

	byKey, err := repo.FindByProviderAndIdempotencyKey(ctx, candidate.ProviderID(), candidate.IdempotencyKey())
	if err != nil {
		return nil, err
	}
	if byKey != nil {
		if byKey.PayloadHash() != candidate.PayloadHash() {
			return nil, domain.ErrIdempotencyConflict
		}
		return byKey, nil
	}

	byExternalID, err := repo.FindByProviderAndExternalTxID(ctx, candidate.ProviderID(), candidate.ExternalTransactionID())
	if err != nil {
		return nil, err
	}
	if byExternalID != nil {
		// Em READ COMMITTED, esta segunda busca e a busca por chave
		// acima não veem necessariamente o mesmo instante do banco:
		// sob corrida, é possível que o INSERT do vencedor já esteja
		// visível aqui (por externalId), mas não estivesse visível na
		// primeira busca (por idempotencyKey), que rodou um instante
		// antes. Sem esta checagem, o perdedor da corrida recebia
		// ErrExternalTransactionConflict por engano — era a MESMA
		// operação, só vista em outro instante.
		if byExternalID.IdempotencyKey() == candidate.IdempotencyKey() {
			if byExternalID.PayloadHash() != candidate.PayloadHash() {
				return nil, domain.ErrIdempotencyConflict
			}
			return byExternalID, nil
		}
		// Chave realmente diferente: este externalId já pertence a
		// outra requisição.
		return nil, domain.ErrExternalTransactionConflict
	}

	return nil, nil
}
