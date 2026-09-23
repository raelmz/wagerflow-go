package domain

import "errors"

// Erros que NÃO são rejeições de negócio persistidas: indicam que a
// requisição em si não pode ser aceita (conflito, dado inconsistente).
// Nenhum deles deixa registro no banco — por isso são erros Go
// classificáveis com errors.Is, e não um WagerStatus.
var (
	// ErrDuplicateTransaction é devolvido pelo repositório quando um
	// INSERT bate num índice único de idempotência (mesma chave, ou
	// mesmo (providerId, externalTransactionId)). É o sinal de que
	// outra requisição igual venceu a corrida — o caso de uso então
	// relê o registro vencedor e trata como replay.
	ErrDuplicateTransaction = errors.New("já existe transação com esta chave de idempotência ou id externo")

	// ErrIdempotencyConflict: a chave de idempotência já foi usada
	// com um conteúdo (hash) diferente. HTTP 409.
	ErrIdempotencyConflict = errors.New("chave de idempotência reutilizada com conteúdo diferente")

	// ErrExternalTransactionConflict: (providerId, externalTransactionId)
	// já foi registrado com OUTRA chave de idempotência. A seção 9 do
	// desafio proíbe reaplicar a mesma operação financeira por outra
	// chave. HTTP 409.
	ErrExternalTransactionConflict = errors.New("operação já registrada com outra chave de idempotência")

	// ErrWalletNotFound: a carteira informada não existe. Sem carteira
	// não há como persistir a transação (wallet_id é FK obrigatória).
	ErrWalletNotFound = errors.New("carteira não encontrada")

	// ErrPlayerWalletMismatch: o playerId da operação não é o dono
	// da carteira informada.
	ErrPlayerWalletMismatch = errors.New("playerId não corresponde ao dono da carteira")
)
