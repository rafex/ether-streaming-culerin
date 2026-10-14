# commit-msg

## Nombre

commit-msg — valida que un mensaje de commit siga Conventional Commits

## Sinopsis

```
just commit-msg <archivo-del-mensaje>
bash helpers/shell/commit-msg.sh --man
```

## Descripción

Invocado por el git hook `.githooks/commit-msg` (regla 10) vía
`just commit-msg "$1"`. Rechaza el commit (`exit 1`) si el mensaje no
cumple el formato `tipo[(scope)][!]: descripción`, o si detecta un
archivo `.env` con contenido en el staging area.

## Opciones

- Tipos válidos: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `build`, `ci`, `perf`, `revert`
- Ignora: merge commits, revert automático, initial commit

## Ejemplos

```bash
just commit-msg --man
echo "feat(auth): agrega login con OAuth" > /tmp/msg && just commit-msg /tmp/msg
```

## Véase también

- [Regla 05: Control de Versiones](https://github.com/rafex/ether-my-best-practice/blob/main/rules/05-version-control_draft.md)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
