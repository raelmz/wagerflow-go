#!/usr/bin/env bash
# deployments/migrate.sh — atalho para rodar o golang-migrate contra o
# Postgres do docker-compose SEM precisar instalar a CLI localmente
# (usa a mesma imagem `migrate/migrate` já usada pelo serviço `migrate`
# do docker-compose.yml, só que sob demanda).
#
# O serviço `migrate` do compose já roda "up" automaticamente toda vez
# que você faz `docker compose up`. Este script serve para os outros
# comandos do dia a dia que o compose não cobre sozinho: descer uma
# migration (down), ver em qual versão o banco está, ou forçar uma
# versão depois de mexer manualmente no banco.
#
# Uso (a partir da RAIZ do repositório, não de dentro de deployments/):
#   ./deployments/migrate.sh up            # aplica tudo que falta
#   ./deployments/migrate.sh down 1        # desfaz a última migration
#   ./deployments/migrate.sh version       # mostra a versão atual
#
# Pré-requisito: o Postgres do compose precisa estar no ar
# (docker compose -f deployments/docker-compose.yml up -d postgres).

set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "Uso: $0 <comando do golang-migrate> [args...]" >&2
  echo "Exemplos: $0 up | $0 down 1 | $0 version" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

docker run --rm \
  --network deployments_default \
  -v "${SCRIPT_DIR}/../migrations:/migrations:ro" \
  migrate/migrate:v4.18.1 \
  -path /migrations \
  -database "postgres://wagerflow:wagerflow@wagerflow-postgres:5432/wagerflow?sslmode=disable" \
  "$@"
