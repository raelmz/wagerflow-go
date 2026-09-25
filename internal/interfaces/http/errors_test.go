package http

import (
	"errors"
	"net/http"
	"testing"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

// TestMapError_ContratoDeStatusPorErro confere, para cada erro que um
// caso de uso pode devolver, o par (status HTTP, code estável) que a
// seção 9 do desafio exige ser "distinguível pelo contrato". Isto é
// o coração do contrato de erro da API — qualquer regressão aqui
// muda o comportamento observável por todo cliente.
func TestMapError_ContratoDeStatusPorErro(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"entrada inválida (camada HTTP)", errInvalidInput{msg: "x"}, http.StatusBadRequest, "INVALID_INPUT"},
		{"isolamento de provider", errForbidden{msg: "x"}, http.StatusForbidden, "PROVIDER_MISMATCH"},
		{"valor monetário inválido", domain.ErrInvalidAmount, http.StatusBadRequest, "INVALID_INPUT"},
		{"valor negativo", domain.ErrNegativeAmount, http.StatusBadRequest, "INVALID_INPUT"},
		{"dados inválidos para o tipo", domain.ErrInvalidWagerData, http.StatusBadRequest, "INVALID_INPUT"},
		{"referência ausente", domain.ErrMissingReference, http.StatusBadRequest, "INVALID_INPUT"},
		{"playerId inválido", domain.ErrInvalidPlayerID, http.StatusBadRequest, "INVALID_INPUT"},
		{"carteira não encontrada", domain.ErrWalletNotFound, http.StatusNotFound, "WALLET_NOT_FOUND"},
		{"transação não encontrada", application.ErrWagerTransactionNotFound, http.StatusNotFound, "TRANSACTION_NOT_FOUND"},
		{"carteira já existe", domain.ErrWalletAlreadyExists, http.StatusConflict, "WALLET_ALREADY_EXISTS"},
		{"conflito de idempotência", domain.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT"},
		{"conflito de transação externa", domain.ErrExternalTransactionConflict, http.StatusConflict, "EXTERNAL_TRANSACTION_CONFLICT"},
		{"transação duplicada (não deveria escapar, mas...)", domain.ErrDuplicateTransaction, http.StatusConflict, "DUPLICATE_TRANSACTION"},
		{"playerId não é dono da carteira", domain.ErrPlayerWalletMismatch, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION"},
		{"moeda incompatível com a carteira", domain.ErrWalletCurrencyMismatch, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION"},
		{"erro não classificado -> indisponibilidade transitória", errors.New("falha inesperada de infraestrutura"), http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := mapError(tt.err)
			if status != tt.wantStatus {
				t.Errorf("status: esperava %d, veio %d", tt.wantStatus, status)
			}
			if body.Code != tt.wantCode {
				t.Errorf("code: esperava %q, veio %q", tt.wantCode, body.Code)
			}
			// message nunca pode vir vazia: é o que o humano/log usa
			// para diagnosticar, mesmo com o code já sendo a parte
			// programática do contrato.
			if body.Message == "" {
				t.Error("message não deveria vir vazia")
			}
		})
	}
}

// TestMapError_ErroEncapsuladoAindaEhClassificavel confere que um erro
// envolto com fmt.Errorf("%w: ...", ...) — como buildCandidate faz em
// vários pontos — continua sendo classificado corretamente por
// errors.Is/errors.As, não caindo no default de 503 por engano.
func TestMapError_ErroEncapsuladoAindaEhClassificavel(t *testing.T) {
	wrapped := errWrap("contexto extra", domain.ErrInvalidWagerData)

	status, body := mapError(wrapped)
	if status != http.StatusBadRequest {
		t.Fatalf("esperava 400 mesmo com o erro encapsulado, veio %d", status)
	}
	if body.Code != "INVALID_INPUT" {
		t.Fatalf("esperava code INVALID_INPUT, veio %q", body.Code)
	}
}

func errWrap(msg string, err error) error {
	return &wrappedErr{msg: msg, err: err}
}

type wrappedErr struct {
	msg string
	err error
}

func (e *wrappedErr) Error() string { return e.msg + ": " + e.err.Error() }
func (e *wrappedErr) Unwrap() error { return e.err }
