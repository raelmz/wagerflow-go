// Pacote config concentra a leitura de variáveis de ambiente. Único
// lugar do projeto que sabe o NOME de cada variável — todo o resto
// recebe valores já resolvidos, nunca chama os.Getenv diretamente
// (isso facilitaria trocar a fonte de configuração no futuro sem
// caçar os.Getenv espalhado pelo código).
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	HTTPPort    string
}

// Load lê as variáveis de ambiente obrigatórias e aplica defaults às
// opcionais. Não chama godotenv.Load() aqui — quem monta a aplicação
// (cmd/api/main.go) decide se/quando carregar o .env, para este
// pacote continuar testável sem depender de arquivo em disco.
func Load() (*Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("variável de ambiente DATABASE_URL é obrigatória")
	}

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{DatabaseURL: databaseURL, HTTPPort: port}, nil
}
