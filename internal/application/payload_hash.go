package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// computePayloadHash calcula o hash determinístico dos CAMPOS DE NEGÓCIO
// de uma operação (seção 9 do desafio).
//
// Algoritmo:
//  1. Monta um objeto JSON só com os campos de negócio (lista abaixo).
//  2. Serializa com as chaves em ORDEM ALFABÉTICA (JSON canônico).
//  3. Aplica SHA-256 e devolve em hexadecimal.
//
// Por que JSON com chaves ordenadas? Para que o mesmo conteúdo gere
// SEMPRE o mesmo hash, não importa a ordem em que o cliente mandou os
// campos. Em Go isso é de graça: json.Marshal de um map[string]string
// ordena as chaves alfabeticamente (está na especificação do pacote).
//
// O que fica FORA do hash, de propósito:
//   - a chave de idempotência (é a "identidade" da requisição, não o
//     conteúdo — o desafio manda excluí-la);
//   - metadados de transporte (headers HTTP, messageId do SQS etc.).
//
// Por isso HTTP e SQS produzem o MESMO hash para a mesma operação:
// ambos os caminhos montam um ProcessWagerCommand e passam por aqui.
//
// Normalizações aplicadas ANTES do hash (seção 6.1 do desafio pede que
// sejam documentadas):
//   - dinheiro: usa Money.String() do valor já parseado, então "25.00"
//     tem uma única forma textual. A moeda entra em maiúsculas
//     ("brl" e "BRL" são a mesma operação);
//   - ids de jogador/carteira: usa uuid.String() (minúsculas, com hífens);
//   - ids textuais (providerId, externalTransactionId, roundId, gameId)
//     NÃO são normalizados: "Round-1" e "round-1" são valores diferentes.
func computePayloadHash(cmd ProcessWagerCommand, money domain.Money) (string, error) {
	fields := map[string]string{
		"providerId":                     cmd.ProviderID,
		"externalTransactionId":          cmd.ExternalTransactionID,
		"playerId":                       cmd.PlayerID.String(),
		"walletId":                       cmd.WalletID.String(),
		"roundId":                        cmd.RoundID,
		"gameId":                         cmd.GameID,
		"kind":                           string(cmd.Kind),
		"amount":                         money.String(),
		"currency":                       money.Currency(),
		"referenceExternalTransactionId": cmd.ReferenceExternalTransactionID,
	}

	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
