package domain

// Códigos de falha ESTÁVEIS de operações REJECTED (seção 7 do desafio:
// "toda rejeição deve fornecer um failureCode estável e documentado").
// São contrato público: clientes podem depender destes textos, então
// não renomeie um código depois de publicado.
//
// Classificação (corrigível x definitivo):
//   - Todos os códigos abaixo são DEFINITIVOS para aquela operação:
//     ela fica REJECTED (terminal) e um replay devolve a mesma
//     rejeição. Para tentar de novo, o provedor deve enviar uma
//     operação NOVA (outro externalTransactionId e outra chave).
//   - Entradas CORRIGÍVEIS (JSON inválido, valor mal formatado,
//     carteira inexistente, moeda diferente da carteira) NÃO viram
//     WagerTransaction: são erros de validação sem nenhum efeito
//     persistido, e o cliente pode reenviar corrigido com a mesma chave.
const (
	// BET sem saldo suficiente na carteira.
	FailureInsufficientBalance = "INSUFFICIENT_BALANCE"

	// Reversão que precisaria DEBITAR mais do que o saldo disponível
	// (ex: ROLLBACK de um WIN cujo prêmio já foi gasto). Código
	// diferente do anterior, como a seção 7 exige.
	FailureReversalInsufficientBalance = "REVERSAL_INSUFFICIENT_BALANCE"

	// A referência não existe e o prazo/tentativas de espera acabaram
	// (usado pelo worker de referências pendentes).
	FailureReferenceNotFound = "REFERENCE_NOT_FOUND"

	// A referência existe, mas terminou sem sucesso (REJECTED/FAILED):
	// esperar não adianta, ela nunca será PROCESSED.
	FailureReferenceNotProcessed = "REFERENCE_NOT_PROCESSED"

	// Operação e referência discordam em provedor, jogador, carteira,
	// moeda ou rodada.
	FailureReferenceMismatch = "REFERENCE_MISMATCH"

	// Combinação de tipos não permitida (ex: REFUND de uma WIN).
	FailureReferenceInvalidKind = "REFERENCE_INVALID_KIND"

	// A referência já recebeu uma reversão bem-sucedida (REFUND ou
	// ROLLBACK). Impede devolver duas vezes o mesmo débito.
	FailureReferenceAlreadyReversed = "REFERENCE_ALREADY_REVERSED"

	// Valor da reversão diferente do valor da operação referenciada
	// (reversões parciais não fazem parte do desafio).
	FailureReversalAmountMismatch = "REVERSAL_AMOUNT_MISMATCH"
)
