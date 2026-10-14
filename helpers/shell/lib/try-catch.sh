#!/usr/bin/env bash
# ------------------------------------------------------------------
# try-catch.sh — Manejo de errores para helpers shell.
# Source: source "$(dirname "${BASH_SOURCE[0]}")/lib/try-catch.sh"
#
# Requiere: logs.sh (debe sourcearse antes).
# Funciones: run_with_guard <command>, catch, on_error <cleanup_func>,
#            fail_fast <message>, ERR trap.
#
# Para agentes de IA: usar run_with_guard para envolver comandos
# críticos y catch para manejar fallos.
# ------------------------------------------------------------------

export LAST_EXIT_CODE=0
export ON_ERROR_CLEANUP=""

# run_with_guard <command...> — ejecuta comando con trap ERR + logging.
run_with_guard() {
	LAST_EXIT_CODE=0
	set +e
	"$@"
	LAST_EXIT_CODE=$?
	set -e
	if [[ $LAST_EXIT_CODE -ne 0 ]]; then
		log_error "Command failed (exit $LAST_EXIT_CODE): $*"
		catch "$LAST_EXIT_CODE" "$*"
	fi
	return $LAST_EXIT_CODE
}

# catch <exit_code> <command> — maneja un fallo.
catch() {
	local code="$1"
	shift
	log_error "Caught error in: $*"
	if [[ -n "$ON_ERROR_CLEANUP" ]]; then
		log_info "Running cleanup: $ON_ERROR_CLEANUP"
		eval "$ON_ERROR_CLEANUP"
	fi
}

# on_error <function_or_cmd> — registra una función de cleanup.
on_error() {
	ON_ERROR_CLEANUP="$1"
}

# fail_fast <message> [exit_code=1] — aborta con mensaje.
fail_fast() {
	local msg="$1"
	local code="${2:-1}"
	log_error "$msg"
	catch "$code" "$msg"
	exit "$code"
}

# Set trap for ERR signal.
trap 'LAST_EXIT_CODE=$?; if [[ $LAST_EXIT_CODE -ne 0 ]]; then log_error "Unexpected error at line $LINENO (exit $LAST_EXIT_CODE)"; if [[ -n "$ON_ERROR_CLEANUP" ]]; then eval "$ON_ERROR_CLEANUP"; fi;' ERR
