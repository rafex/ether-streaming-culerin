#!/usr/bin/env bash
# hooks.sh — Solo instalación de git hooks.
# No despacha gates. Cada hook tiene su propio script (pre-commit.sh, pre-push.sh).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

action="${1:-}"

case "$action" in
	install)
		git config core.hooksPath .githooks
		log_info "Git hooks instalados: core.hooksPath = .githooks"
		;;
	*)
		echo "Usage: hooks.sh install" >&2
		exit 1
		;;
esac
