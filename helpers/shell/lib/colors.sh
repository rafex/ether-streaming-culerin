#!/usr/bin/env bash
# ------------------------------------------------------------------
# colors.sh — Colores ANSI para helpers shell.
# Source: source "$(dirname "${BASH_SOURCE[0]}")/lib/colors.sh"
#
# Variables: COLOR_RED, COLOR_GREEN, COLOR_YELLOW, COLOR_BLUE,
# COLOR_CYAN, COLOR_MAGENTA, COLOR_BOLD, COLOR_RESET.
# Funciones: colorize <color> <text>, color_enabled (auto-off en no-TTY).
#
# Para agentes de IA: usar colores vía colorize/COLOR_* para mensajes
# de UI; colors.sh + messages.sh = max reutilización.
# ------------------------------------------------------------------

export COLOR_RED=""
export COLOR_GREEN=""
export COLOR_YELLOW=""
export COLOR_BLUE=""
export COLOR_CYAN=""
export COLOR_MAGENTA=""
export COLOR_BOLD=""
export COLOR_RESET=""
export COLOR_ENABLED=false

_init_colors() {
	if [[ -t 1 ]] && [[ -t 2 ]]; then
		COLOR_ENABLED=true
		COLOR_RED='\033[0;31m'
		COLOR_GREEN='\033[0;32m'
		COLOR_YELLOW='\033[1;33m'
		COLOR_BLUE='\033[0;34m'
		COLOR_CYAN='\033[0;36m'
		COLOR_MAGENTA='\033[0;35m'
		COLOR_BOLD='\033[1m'
		COLOR_RESET='\033[0m'
	fi
}

_init_colors

# colorize <color_var_name> <text>
colorize() {
	local color_var="$1"
	local text="$2"
	if [[ "$COLOR_ENABLED" == "true" ]]; then
		echo -e "${!color_var}${text}${COLOR_RESET}"
	else
		echo "$text"
	fi
}

# color_enabled → true si la terminal soporta colores
color_enabled() {
	[[ "$COLOR_ENABLED" == "true" ]]
}
