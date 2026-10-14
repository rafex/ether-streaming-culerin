#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/messages.sh"

# ------------------------------------------------------------------
# Validador de Conventional Commits para git hook commit-msg.
# Llamado por hooks.sh --action commit-msg.
#
# Regla 05: todo commit debe seguir Conventional Commits.
# Formato: type(scope)[!]: description
#
# Tipos válidos: feat|fix|docs|style|refactor|test|chore|build|ci|perf|revert
#
# Ignora: merge commits, initial commit, revert automático.
#
# Uso:
#   bash helpers/shell/commit-msg.sh <archivo-del-mensaje>
#   bash helpers/shell/commit-msg.sh --man   # Manual (docs/man_commit-msg.md)
#   exit 0 → commit válido
#   exit 1 → commit rechazado
# ------------------------------------------------------------------

if [[ "${1:-}" == "--man" ]]; then
	show_man "commit-msg"
	exit 0
fi

msg_file="${1:-}"
if [[ -z "$msg_file" || ! -f "$msg_file" ]]; then
	echo "commit-msg: no se recibió archivo de mensaje de commit" >&2
	exit 0
fi

msg="$(head -1 "$msg_file" 2>/dev/null || true)"

# Ignorar merges, revert automáticos e initial commit
if [[ "$msg" =~ ^Merge ]] || [[ "$msg" =~ ^Revert ]] || [[ "$msg" == "Revert "* ]]; then
	exit 0
fi
if echo "$msg" | grep -qE '^Initial commit'; then
	exit 0
fi

# Validar formato Conventional Commits
if echo "$msg" | grep -qE '^(feat|fix|docs|style|refactor|test|chore|build|ci|perf|revert)(\([a-zA-Z0-9._-]+\))?!?: .+'; then
	# Doble-check: rechazar .env / secretos en plano staged
	staged_env="$(git diff --cached --name-only 2>/dev/null | grep -E '(^|/)(\.env$|\.env\.[a-z]+$)' || true)"
	if [[ -n "$staged_env" ]]; then
		for f in $staged_env; do
			case "$f" in
				*.env.example) continue ;;
			esac
			echo "" >&2
			echo "ERROR: archivo de entorno con contenido staged: $f" >&2
			echo "Los secretos van en .secrets/*.enc.yaml; las variables públicas en .enviroments/." >&2
			echo "Usa 'just env-link <env>' para el symlink .env." >&2
			exit 1
		done
	fi
	exit 0
fi

echo "" >&2
echo "ERROR: el mensaje de commit no sigue Conventional Commits." >&2
echo "" >&2
echo "Formato esperado: tipo[(scope)][!]: descripción" >&2
echo "Tipos válidos:    feat, fix, docs, style, refactor, test, chore," >&2
echo "                   build, ci, perf, revert" >&2
echo "" >&2
echo "Tu mensaje:       $msg" >&2
echo "" >&2
exit 1
