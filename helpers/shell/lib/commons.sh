#!/usr/bin/env bash
# ------------------------------------------------------------------
# commons.sh — Utilidades comunes para helpers shell.
# Source: source "$(dirname "${BASH_SOURCE[0]}")/lib/commons.sh"
#
# Proporciona: set -euo pipefail, SCRIPT_DIR, PROJECT_NAME, WORKSPACE,
# parse_common_flags, utilidades de archivos.
#
# Para agentes de IA: sourcear esta lib al inicio de cualquier helper
# shell para obtener el entorno base y evitar duplicación.
# ------------------------------------------------------------------
set -euo pipefail

export SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export WORKSPACE="${WORKSPACE:-$(pwd)}"
export PROJECT_NAME="${PROJECT_NAME:-$(basename "$WORKSPACE")}"

# Parse common flags shared by all helpers.
# After calling this, the following variables are set:
#   $action, $goal, $log_file, $log_level, $command_line
# All others remain in $@ for manual parsing.
parse_common_flags() {
	action=""
	goal=""
	log_file=""
	log_level="info"
	command_line=""
	local remaining=()

	while [[ $# -gt 0 ]]; do
		case "$1" in
			--action)     action="${2:-}"; shift 2 ;;
			--goal)       goal="${2:-}"; shift 2 ;;
			--log-file)   log_file="${2:-}"; shift 2 ;;
			--log-level)  log_level="${2:-}"; shift 2 ;;
			--command)    command_line="${2:-}"; shift 2 ;;
			--workspace)  WORKSPACE="${2:-}"; shift 2 ;;
			--project-name) PROJECT_NAME="${2:-}"; shift 2 ;;
			--help|-h)
				cat <<'EOF'
Usage: $0 --action <action> [--log-file /path/to/log] [--log-level info] ...
Common flags:
  --action, --goal     Operation to execute
  --log-file           Audit log path (required)
  --log-level          Log level (default: info)
  --command            Raw command (overrides action/goal)
  --workspace          Working directory (default: pwd)
EOF
				exit 0
				;;
			*)
				remaining+=("$1"); shift ;;
		esac
	done
	set -- "${remaining[@]}"
}

# Determine the effective action/goal after --command override.
effective_action() {
	if [[ -n "$command_line" ]]; then
		echo ""
	elif [[ -n "$action" ]]; then
		echo "$action"
	elif [[ -n "$goal" ]]; then
		echo "$goal"
	else
		echo ""
	fi
}

# Ensure a directory exists, with optional permission check.
ensure_dir() {
	local dir="$1"
	local label="${2:-directory}"
	if [[ ! -d "$dir" ]]; then
		mkdir -p "$dir"
	fi
}
