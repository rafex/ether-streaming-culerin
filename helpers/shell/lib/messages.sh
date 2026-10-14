#!/usr/bin/env bash
# ------------------------------------------------------------------
# messages.sh — Mensajes de UI para helpers shell.
# Source: source "$(dirname "${BASH_SOURCE[0]}")/lib/messages.sh"
#
# Requisito: logs.sh (debe sourcearse antes).
# Auto-sourcea colors.sh si no se ha hecho.
# Funciones: success, error, warning, info, step, die, header.

# Auto-source colors.sh si no se ha hecho
if [[ -z "${COLOR_ENABLED:-}" ]]; then
    _lib_dir="$(dirname "${BASH_SOURCE[0]:-$0}")"
    if [[ -f "$_lib_dir/colors.sh" ]]; then
        source "$_lib_dir/colors.sh"
    fi
fi
# ------------------------------------------------------------------

# success <message>
success() { echo -e "$(colorize COLOR_GREEN "✓") $(log_info "$@")"; }

# error <message>
error()  { echo -e "$(colorize COLOR_RED "✗") $(log_error "$@")" >&2; }

# warning <message>
warning(){ echo -e "$(colorize COLOR_YELLOW "⚠") $(log_warn "$@")"; }

# info <message>
info()   { echo -e "$(colorize COLOR_CYAN "ℹ") $(log_info "$@")"; }

# step <N> <total> <message>  → "step 2/5: message"
step() {
	local n="$1"
	local total="$2"
	shift 2
	echo -e "$(colorize COLOR_BLUE "▶") [${n}/${total}] $*"
}

# die <message> [exit_code=1]
die() {
	local msg="$1"
	local code="${2:-1}"
	error "$msg"
	exit "$code"
}

# header <message>  → línea decorativa en negrita
header() {
	local msg="$1"
	local line
	printf -v line '%*s' "${#msg}" ''
	line="${line// /─}"
	echo
	echo "$(colorize COLOR_BOLD "$msg")"
	echo "$(colorize COLOR_BOLD "$line")"
}

# show_man <tarea>  → muestra docs/man_<tarea>.md y termina la llamada (regla 01).
# El manual vive siempre en docs/, nunca embebido en el helper.
show_man() {
	local task="$1"
	local root="${WORKSPACE:-$(pwd)}"
	local man_file="$root/docs/man_${task}.md"
	if [[ ! -f "$man_file" ]]; then
		error "No existe manual para '$task'. Esperado: docs/man_${task}.md"
		exit 1
	fi
	if command -v mdcat >/dev/null 2>&1; then
		mdcat --ansi --no-pager "$man_file"
	elif command -v glow >/dev/null 2>&1; then
		glow "$man_file"
	elif command -v bat >/dev/null 2>&1; then
		bat --plain --language markdown --color=always --paging=never "$man_file"
	elif [[ -t 1 ]] && command -v less >/dev/null 2>&1; then
		less -R "$man_file"
	else
		cat "$man_file"
	fi
}
