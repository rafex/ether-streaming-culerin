#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

# ------------------------------------------------------------------
# docs.sh — Documentación como código (MkDocs Material + MermaidJS).
#
# Contrato (regla 01):
#   --goal build|serve                  Operación
#   --file .config/mkdocs/mkdocs.yml    Configuración MkDocs
#   --site-dir site                     Salida del sitio
#   --command "<cmd>"                   Comando directo (sobreescribe goal)
#   --log-file /ruta/al/log  --log-level info  --man
#
# Usa `mkdocs` si está instalado; si no, `uvx` con las dependencias de
# .config/mkdocs/requirements.txt (sin instalar nada globalmente).
# ------------------------------------------------------------------

goal=""
mkdocs_file=".config/mkdocs/mkdocs.yml"
site_dir="site"
command_line=""
man="false"
WORKSPACE="${WORKSPACE:-$(pwd)}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--goal)      goal="${2:-}"; shift 2 ;;
		--file)      mkdocs_file="${2:-}"; shift 2 ;;
		--site-dir)  site_dir="${2:-}"; shift 2 ;;
		--command)   command_line="${2:-}"; shift 2 ;;
		--log-file)  LOG_FILE="${2:-}"; shift 2 ;;
		--log-level) LOG_LEVEL="${2:-}"; shift 2 ;;
		--man)       man="true"; shift ;;
		*)           echo "Unknown flag: $1" >&2; exit 1 ;;
	esac
done

if [[ "$man" == "true" ]]; then
	show_man "docs"
	exit 0
fi
if [[ -n "$command_line" ]]; then goal=""; fi
[[ -n "$goal" || -n "$command_line" ]] || { echo "Missing required flag --goal or --command" >&2; exit 1; }

init_log "docs"

mkdocs_cmd() {
	if command -v mkdocs >/dev/null 2>&1; then
		echo "mkdocs"
	elif command -v uvx >/dev/null 2>&1; then
		echo "uvx --with-requirements $WORKSPACE/.config/mkdocs/requirements.txt mkdocs"
	else
		die "mkdocs no encontrado. Instala uv (recomendado) o: pip install -r .config/mkdocs/requirements.txt"
	fi
}

if [[ -n "$command_line" ]]; then
	cmd="$command_line"
else
	mk="$(mkdocs_cmd)"
	case "$goal" in
		build) cmd="$mk build --strict -f $mkdocs_file --site-dir $WORKSPACE/$site_dir" ;;
		serve) cmd="$mk serve -f $mkdocs_file" ;;
		*)     die "Unknown goal: $goal" ;;
	esac
fi

cd "$WORKSPACE"
log_info "Executing: $cmd"
bash -c "$cmd"
