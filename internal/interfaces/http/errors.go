package http

import (
	"errors"
	"net/http"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

// errInvalidInput representa um erro de entrada detectado NESTA
// camada (JSON malformado, UUID inválido, campo obrigatório ausente)
// — ainda não chegou nem perto do domínio. Sempre 400.
type errInvalidInput struct{ msg string }

func (e errInvalidInput) Error() string { return e.msg }

// apiErrorBody é o corpo de erro padrão da API. `code` é estável e
// pensado para o cliente decidir programaticamente o que fazer;
// `message` é só para humano/log.
type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// mapError traduz um erro devolvido por um caso de uso no par
// (status HTTP, corpo) que a seção 9 do desafio exige ser
// "distinguível pelo contrato" para cada situação:
//
//	400 Bad Request           -> entrada inválida (JSON malformado, dado ausente/mal formatado)
//	404 Not Found              -> carteira ou transação não encontrada numa CONSULTA (GET)
//	409 Conflict                -> conflito de idempotência, carteira duplicada, corrida sem vencedor
//	422 Unprocessable Entity   -> dados coerentes mas que a REGRA DE NEGÓCIO do domínio recusa
//	503 Service Unavailable    -> qualquer outra falha (infraestrutura, transitória)
//
// Rejeição de negócio DENTRO de um processamento (ex: saldo
// insuficiente numa BET) não passa por aqui: ela não é um erro Go,
// volta no ProcessWagerResult com status REJECTED e HTTP 200/201 —
// a operação foi aceita e processada, só que o resultado é "recusada".
func mapError(err error) (int, apiErrorBody) {
	switch {
	case errors.As(err, &errInvalidInput{}):
		return http.StatusBadRequest, apiErrorBody{Code: "INVALID_INPUT", Message: err.Error()}

	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrNegativeAmount),
		errors.Is(err, domain.ErrInvalidWagerData),
		errors.Is(err, domain.ErrMissingReference),
		errors.Is(err, domain.ErrInvalidPlayerID):
		return http.StatusBadRequest, apiErrorBody{Code: "INVALID_INPUT", Message: err.Error()}

	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, apiErrorBody{Code: "WALLET_NOT_FOUND", Message: err.Error()}

	case errors.Is(err, application.ErrWagerTransactionNotFound):
		return http.StatusNotFound, apiErrorBody{Code: "TRANSACTION_NOT_FOUND", Message: err.Error()}

	case errors.Is(err, domain.ErrWalletAlreadyExists):
		return http.StatusConflict, apiErrorBody{Code: "WALLET_ALREADY_EXISTS", Message: err.Error()}

	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict, apiErrorBody{Code: "IDEMPOTENCY_KEY_CONFLICT", Message: err.Error()}

	case errors.Is(err, domain.ErrExternalTransactionConflict):
		return http.StatusConflict, apiErrorBody{Code: "EXTERNAL_TRANSACTION_CONFLICT", Message: err.Error()}

	case errors.Is(err, domain.ErrDuplicateTransaction):
		// Não deveria escapar até aqui (o caso de uso trata isso
		// internamente, relendo o vencedor da corrida) — mas se
		// escapar por algum caminho não previsto, ainda é conflito.
		return http.StatusConflict, apiErrorBody{Code: "DUPLICATE_TRANSACTION", Message: err.Error()}

	case errors.Is(err, domain.ErrPlayerWalletMismatch),
		errors.Is(err, domain.ErrWalletCurrencyMismatch):
		return http.StatusUnprocessableEntity, apiErrorBody{Code: "BUSINESS_RULE_VIOLATION", Message: err.Error()}

	default:
		// Qualquer coisa não classificada é tratada como indisponibilidade
		// transitória (falha de banco, timeout, etc.) — o cliente pode
		// tentar de novo com segurança, já que nada foi persistido
		// (o caso de uso só chega a persistir dentro de uma transação
		// que teria feito rollback nesse erro).
		return http.StatusServiceUnavailable, apiErrorBody{Code: "SERVICE_UNAVAILABLE", Message: "falha temporária, tente novamente"}
	}
}
