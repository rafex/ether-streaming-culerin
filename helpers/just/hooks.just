# helpers/just/hooks.just — Operativas de git hooks.
# commit-msg es operativa de mantenimiento → Justfile.

# Validar convención de commits (operativa de mantenimiento, no construcción)
# *args se reenvía al helper: `just commit-msg --man` muestra docs/man_commit-msg.md
@commit-msg *args:
    bash helpers/shell/commit-msg.sh {{args}}
