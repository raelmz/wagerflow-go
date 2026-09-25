// Package observability concentra o que o desafio pede na seção 12
// ("Observabilidade"): logs em JSON com os identificadores de
// rastreio (correlationId, messageId, transactionId, walletId,
// providerId), sem credenciais nem payload financeiro completo.
//
// Por que log/slog e não uma lib externa (zap, zerolog, logrus)?
// slog é da standard library do Go desde a versão 1.21 — já entrega
// JSON estruturado, níveis (Info/Warn/Error) e "campos" (Attrs) sem
// precisar adicionar mais uma dependência ao go.mod. Os 4 binários do
// projeto (api, outbox-publisher, wager-consumer,
// pending-reference-worker) usam exatamente o mesmo logger, só
// trocando o campo "service".
package observability

import (
	"log/slog"
	"os"
)

// Nomes de campo padronizados para os IDs de rastreio pedidos pela
// seção 12 do desafio. Usar constantes em vez de strings soltas em
// cada chamada evita, por exemplo, logar "transaction_id" num lugar e
// "transactionId" em outro — o que quebraria uma busca/filtro nos
// logs.
const (
	FieldCorrelationID = "correlationId"
	FieldMessageID     = "messageId"
	FieldTransactionID = "transactionId"
	FieldWalletID      = "walletId"
	FieldProviderID    = "providerId"
	FieldError         = "error"
)

// NewLogger cria o *slog.Logger usado por um binário inteiro.
//
// service identifica QUAL processo está logando (ex.: "api",
// "outbox-publisher") — com os 4 binários rodando ao mesmo tempo no
// docker compose (todos escrevendo em stdout, coletados juntos pelo
// `docker compose logs`), esse campo é o que permite filtrar "só os
// logs da API" ou "só os logs do publisher".
//
// O nível vem da variável de ambiente LOG_LEVEL (default: "info").
// "debug" liga logs mais verbosos; qualquer outro valor (ou ausência
// da variável) mantém o padrão "info", que é o nível de produção.
func NewLogger(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: levelFromEnv(),
	})

	return slog.New(handler).With(slog.String("service", service))
}

func levelFromEnv() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
