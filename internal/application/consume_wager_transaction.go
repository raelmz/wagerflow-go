package application

import (
	"errors"
	"fmt"

	"context"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// InboundWagerMessage é o que o consumidor SQS entrega ao caso de uso,
// já traduzido do envelope de transporte: MessageID vem do próprio
// SQS (atributo MessageId da mensagem recebida, estável entre
// reentregas da mesma entrega lógica), e Command é o MESMO struct que
// o handler HTTP monta — é o que garante que o hash de idempotência
// (seção 9) e todas as regras de negócio sejam idênticas nos dois
// canais.
type InboundWagerMessage struct {
	MessageID string
	Command   ProcessWagerCommand
}

// ConsumeWagerTransactionUseCase processa uma mensagem SQS de
// operação de apostas com deduplicação por inbox (seção 6.5 e 10).
//
// Diferença central para ProcessWagerTransactionUseCase: aqui a
// checagem "esta mensagem já foi vista?" e o efeito financeiro
// acontecem NA MESMA transação SQL — se a inbox for gravada mas o
// efeito falhar, os dois desfazem juntos, e a mensagem pode ser
// reentregue com segurança.
type ConsumeWagerTransactionUseCase struct {
	txRunner     domain.TxRunner
	consumerName string
}

func NewConsumeWagerTransactionUseCase(txRunner domain.TxRunner, consumerName string) *ConsumeWagerTransactionUseCase {
	return &ConsumeWagerTransactionUseCase{txRunner: txRunner, consumerName: consumerName}
}

// Execute processa a mensagem. Um resultado nil (sem erro) significa
// "mensagem já tinha sido processada antes por este consumidor" — o
// chamador deve confirmar (ack/delete) a mensagem sem tratar isso
// como reprocessamento.
//
// Erros devolvidos seguem a MESMA classificação de
// ProcessWagerTransactionUseCase.Execute (ver comentário lá):
// validação/conflito são permanentes (a mensagem deve ir para a DLQ,
// reentregá-la não muda o resultado); qualquer outro erro é
// transitório (a mensagem deve voltar à fila para nova tentativa).
func (uc *ConsumeWagerTransactionUseCase) Execute(ctx context.Context, msg InboundWagerMessage) (*ProcessWagerResult, error) {
	candidate, err := buildCandidate(msg.Command)
	if err != nil {
		return nil, err
	}

	var result *ProcessWagerResult
	var alreadySeen bool

	err = uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		inserted, err := uow.Inbox().TryInsert(ctx, uc.consumerName, msg.MessageID, candidate.PayloadHash())
		if err != nil {
			return err
		}
		if !inserted {
			alreadySeen = true
			return nil
		}

		existing, err := findExisting(ctx, uow, candidate)
		if err != nil {
			return err
		}
		if existing != nil {
			result = &ProcessWagerResult{Transaction: existing, Replay: true}
			return nil
		}

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

		if err := uow.WagerTransactions().Create(ctx, candidate); err != nil {
			return err
		}
		if err := applyWagerTransaction(ctx, uow, candidate); err != nil {
			return err
		}

		result = &ProcessWagerResult{Transaction: candidate, Replay: false}
		return nil
	})

	// Mesma corrida descrita em ProcessWagerTransactionUseCase.Execute:
	// duas entregas (potencialmente de duas instâncias do consumidor)
	// tentaram inserir a mesma transação ao mesmo tempo. Relemos o
	// vencedor numa transação nova — a inbox da entrega perdedora já
	// foi commitada por ela mesma antes de perder a corrida do lado
	// da transação de negócio, o que é seguro: mensagens iguais desta
	// entrega específica não vão reaparecer, e a próxima leitura abaixo
	// devolve o resultado correto de qualquer forma.
	if errors.Is(err, domain.ErrDuplicateTransaction) {
		tmp := &ProcessWagerTransactionUseCase{txRunner: uc.txRunner}
		return tmp.replayInTransaction(ctx, candidate)
	}
	if err != nil {
		return nil, err
	}
	if alreadySeen {
		return nil, nil
	}
	return result, nil
}

// IsPermanentWagerError classifica um erro devolvido por Execute:
// true significa que reentregar a mensagem NUNCA vai mudar o
// resultado (dado inválido, carteira incoerente, conflito de
// idempotência) — a mensagem deve ir para a DLQ. false é falha
// transitória de infraestrutura — a mensagem deve voltar para a fila.
func IsPermanentWagerError(err error) bool {
	if err == nil {
		return false
	}
	permanent := []error{
		domain.ErrInvalidAmount,
		domain.ErrNegativeAmount,
		domain.ErrCurrencyMismatch,
		domain.ErrAmountOverflow,
		domain.ErrInvalidWagerData,
		domain.ErrMissingReference,
		domain.ErrWalletNotFound,
		domain.ErrPlayerWalletMismatch,
		domain.ErrWalletCurrencyMismatch,
		domain.ErrIdempotencyConflict,
		domain.ErrExternalTransactionConflict,
	}
	for _, p := range permanent {
		if errors.Is(err, p) {
			return true
		}
	}
	var invalidMsg errInvalidMessage
	return errors.As(err, &invalidMsg)
}

// errInvalidMessage marca um corpo de mensagem SQS que não pôde nem
// ser decodificado/traduzido para ProcessWagerCommand (JSON malformado,
// campo obrigatório ausente no envelope de transporte). É permanente
// pelo mesmo motivo dos erros de validação do domínio.
type errInvalidMessage struct{ msg string }

func (e errInvalidMessage) Error() string { return e.msg }

func NewInvalidMessageError(format string, args ...any) error {
	return errInvalidMessage{msg: fmt.Sprintf(format, args...)}
}
