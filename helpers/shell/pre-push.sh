#!/usr/bin/env bash
# pre-push.sh — Gate de test (construcción → Makefile).
# Llamado por make pre-push → pre-push.mk.
# Orquesta scripts atómicos: test, trufflehog.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"
init_log "pre-push"

step 1 2 "Ejecutando tests..."
make test || die "test falló"

step 2 2 "Verificación de historial de secretos..."
if command -v trufflehog >/dev/null 2>&1; then
    trufflehog git file://. --no-verification --fail 2>&1 || die "trufflehog detectó posibles secretos"
    log_info "trufflehog: sin secretos detectados."
else
    log_info "trufflehog no instalado — verificación saltada."
fi

success "pre-push superado."
