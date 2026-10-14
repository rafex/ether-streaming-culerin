#!/usr/bin/env bash
# ------------------------------------------------------------------
# logs.sh — Logging con auditoría para helpers shell.
# Source: source "$(dirname "${BASH_SOURCE[0]}")/lib/logs.sh"
#
# Proporciona: init_log <script_name>, log_info/warn/error/debug.
# Centraliza el bloque duplicado de init_log en todos los helpers.
# Resolución: /var/log/<proyecto>/ → fallback /tmp/<proyecto>/.
#
# Para agentes de IA: TODOS los helpers deben llamar init_log en vez
# de implementar el bloque manual de log. Esta función es la fuente única.
# ------------------------------------------------------------------

export LOG_FILE=""
export LOG_LEVEL="${LOG_LEVEL:-info}"

# Inicializa el archivo de log de auditoría.
# $1: nombre del script (p.ej. "hooks", "lint", "cz")
init_log() {
	local script_name="${1:-unknown}"

	if [[ -z "${PROJECT_NAME:-}" ]]; then
		PROJECT_NAME="$(basename "${WORKSPACE:-$(pwd)}")"
	fi

	local ts
	ts="$(date -u +%Y%m%dT%H%M%SZ)"

	if [[ -z "$LOG_FILE" ]]; then
		if mkdir -p "/var/log/$PROJECT_NAME" 2>/dev/null && [[ -w "/var/log/$PROJECT_NAME" ]]; then
			LOG_FILE="/var/log/$PROJECT_NAME/log-$script_name-$ts.log"
		else
			mkdir -p "/tmp/$PROJECT_NAME"
			LOG_FILE="/tmp/$PROJECT_NAME/log-$script_name-$ts.log"
		fi
	fi

	mkdir -p "$(dirname "$LOG_FILE")"
	exec > >(tee -a "$LOG_FILE") 2>&1
	echo "Audit log: $LOG_FILE"
}

# log_info <message>
log_info()  { echo "[INFO]  $(date -u +%T) $*"; }

# log_warn <message>
log_warn()  { echo "[WARN]  $(date -u +%T) $*" >&2; }

# log_error <message>
log_error() { echo "[ERROR] $(date -u +%T) $*" >&2; }

# log_debug <message> (solo si LOG_LEVEL=debug)
log_debug() { if [[ "$LOG_LEVEL" == "debug" ]]; then echo "[DEBUG] $(date -u +%T) $*"; fi; }
