// Pacote postgres contém tudo que fala diretamente com o banco.
// Repare que SÓ este pacote (dentro de infrastructure) sabe o que é
// "pgx" ou "SQL" — o domain nunca importa nada daqui. É essa direção
// de dependência (infrastructure → domain, nunca o contrário) que
// deixa o domínio testável sem precisar de banco nenhum.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool cria um "pool" de conexões com o Postgres — em vez de abrir
// uma conexão nova a cada consulta (caro), o pgx mantém várias
// conexões já abertas e as reaproveita entre requisições concorrentes.
//
// --- Conceito Go 10: context.Context ---
// Quase toda função que faz I/O (banco, rede, arquivo) em Go recebe
// um "context.Context" como primeiro parâmetro. Ele carrega prazos
// (timeout) e permite cancelamento — por exemplo, se uma requisição
// HTTP for cancelada pelo cliente, o context propaga isso até aqui
// e a query no banco pode ser abortada, em vez de rodar até o fim
// à toa. O desafio pede isso explicitamente na seção 6.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar pool de conexões: %w", err)
	}

	// Ping confirma que dá pra conversar com o banco AGORA, não só
	// que a string de conexão tem formato válido.
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("falha ao conectar ao postgres: %w", err)
	}

	return pool, nil
}
