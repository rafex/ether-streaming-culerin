#!/usr/bin/env bash
# pre-commit.sh — Gate de calidad (construcción → Makefile).
# Llamado por make pre-commit → pre-commit.mk.
# Orquesta scripts atómicos: lint (gofmt + vet), secretos.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"
init_log "pre-commit"

module="${MODULE:-source/backend/go/bridge}"

step 1 3 "Lint (gofmt + go vet)..."
bash "$SCRIPT_DIR/go.sh" --goal lint --module "$module" || die "lint falló"

step 2 3 "Verificación de secretos..."
if command -v gitleaks >/dev/null 2>&1; then
	gitleaks git --staged
	log_info "gitleaks: sin secretos detectados."
else
	log_info "gitleaks no instalado — verificación saltada."
fi

step 3 3 "Doble-check de .env y secretos en plano..."
staged_env="$(git diff --cached --name-only | grep -E '(^|/)(\.env$|\.env\.[a-z]+$)' || true)"
for f in $staged_env; do
	case "$f" in
		*.env.example) continue ;;
	esac
	die "Archivo .env staged ($f). Los secretos no se versionan."
done

success "pre-commit superado."
