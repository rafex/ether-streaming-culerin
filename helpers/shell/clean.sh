#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"
init_log "clean"

# ------------------------------------------------------------------
# clean.sh — Limpieza segura de artefactos generados.
#
# Contrato (regla 01):
#   --paths "site mcp/dist ..."   Rutas a limpiar (separadas por espacio)
#   --log-file /ruta/al/log       Obligatorio
#   --log-level info              (opcional)
#
# Guards de seguridad: rechaza rutas peligrosas.
# Auditoría: helpers/shell/lib/logs.sh (regla 15)
# ------------------------------------------------------------------

paths="site"
workspace="$(pwd)"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--paths)     paths="${2:-}"; shift 2 ;;
		--workspace) workspace="${2:-}"; shift 2 ;;
		--log-file)  log_file="${2:-}"; shift 2 ;;
		--log-level) log_level="${2:-}"; shift 2 ;;
		*)
			die "Unknown flag: $1"
			;;
	esac
done

log_info "Clean paths: $paths"
log_info "Workspace: $workspace"

cleaned=0
for path in $paths; do
	# Guards de seguridad
	if [[ -z "$path" || "$path" == "/" || "$path" == "." || "$path" == ".." ]]; then
		warning "Path rechazado por seguridad: '$path'"
		continue
	fi
	# Rechazar rutas absolutas fuera del workspace
	if [[ "$path" == /* && "$path" != "$workspace"* ]]; then
		warning "Path absoluto fuera del workspace rechazado: '$path'"
		continue
	fi
	# Rechazar directorios/patterns que empiecen con ..
	if [[ "$path" == ..* ]]; then
		warning "Path rechazado (backtrack): '$path'"
		continue
	fi
	if [[ -e "$path" ]]; then
		log_info "Removiendo: $path"
		rm -rf "$path"
		cleaned=$((cleaned + 1))
	else
		log_info "No existe: $path (saltando)"
	fi
done

success "Clean completado: $cleaned path(s) removidos."
