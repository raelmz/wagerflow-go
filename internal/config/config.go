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

	// KeycloakIssuerURL é a issuer URL do realm do Keycloak (ex.:
	// http://localhost:8081/realms/wagerflow, vista do HOST, já que
	// a API roda fora de container por enquanto — não existe
	// Dockerfile ainda). A partir dela o middleware de auth faz
	// discovery OIDC sozinho (busca jwks_uri em
	// /.well-known/openid-configuration), então não precisamos de
	// mais nenhuma variável de ambiente para achar as chaves públicas.
	KeycloakIssuerURL string

	// SQSEndpointURL e AWSRegion são usados só pelo GET /health/ready
	// (seção 9 do desafio: readiness precisa cobrir Postgres E SQS,
	// não só o banco). A API não publica nem consome mensagens
	// diretamente — quem faz isso são os outros 3 binários —, então
	// aqui essas variáveis só existem para montar um client de
	// "ping" contra o SQS/LocalStack.
	SQSEndpointURL string
	AWSRegion      string
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

	keycloakIssuerURL := os.Getenv("KEYCLOAK_ISSUER_URL")
	if keycloakIssuerURL == "" {
		return nil, fmt.Errorf("variável de ambiente KEYCLOAK_ISSUER_URL é obrigatória")
	}

	sqsEndpointURL := os.Getenv("SQS_ENDPOINT_URL")
	if sqsEndpointURL == "" {
		return nil, fmt.Errorf("variável de ambiente SQS_ENDPOINT_URL é obrigatória")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		DatabaseURL:       databaseURL,
		HTTPPort:          port,
		KeycloakIssuerURL: keycloakIssuerURL,
		SQSEndpointURL:    sqsEndpointURL,
		AWSRegion:         region,
	}, nil
}
