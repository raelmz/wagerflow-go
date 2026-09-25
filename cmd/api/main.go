// cmd/api é o ponto de entrada da API HTTP. A composição do Fx (quem
// depende de quem) mora em internal/bootstrap — aqui fica só o load
// do .env e o Run(). Isso existe para que a composição possa ser
// testada de fora (seção 13 do desafio): um pacote "main" não pode
// ser importado por nenhum outro pacote em Go, nem por um teste, e
// por isso a composição precisa morar num pacote exportável.
package main

import (
	"github.com/joho/godotenv"
	"go.uber.org/fx"

	"github.com/raelmz/wagerflow-go/internal/bootstrap"
)

func main() {
	// Carregado ANTES do fx.New: se não existir .env (ex: produção,
	// onde as variáveis vêm do ambiente de verdade), godotenv.Load
	// simplesmente falha em silêncio — config.Load() é quem realmente
	// valida o que é obrigatório.
	_ = godotenv.Load()

	fx.New(bootstrap.Module).Run()
}
