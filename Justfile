# Justfile — task manager de operativas (regla 01). No es proxy de make:
# las recetas componen pasos o capturan parámetros y delegan en helpers/.
# Construcción/artefactos → Makefile (make help).

set dotenv-load

module := env_var_or_default("MODULE", "source/backend/go/bridge")

# Listar operativas disponibles
@default:
    just --list

import 'helpers/just/app.just'
import 'helpers/just/hooks.just'

# Preparar el entorno local: módulos Go + git hooks
setup:
    bash helpers/shell/hooks.sh install
    bash helpers/shell/go.sh --goal install --module {{module}}

# Gate completo local antes de abrir un PR: lint + build + cobertura
verify:
    make lint
    make build
    make coverage
