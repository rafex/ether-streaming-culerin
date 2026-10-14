#!/usr/bin/env bash
# man.sh — Muestra el manual de una tarea (docs/man_<tarea>.md).
# Punto de entrada dedicado para `make man TARGET=<target>`, ya que Make no
# reenvía flags arbitrarios (--man) a un target (regla 01).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

target=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--target)
			target="${2:-}"; shift 2 ;;
		*)
			echo "Unknown flag: $1" >&2
			exit 1 ;;
	esac
done

if [[ -z "$target" ]]; then
	echo "Missing required flag --target" >&2
	echo "Usage: man.sh --target <tarea>" >&2
	exit 1
fi

show_man "$target"
