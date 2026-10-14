# coverage

## Nombre

coverage — tests con cobertura y umbral

## Sinopsis

```
make coverage [MODULE=<ruta>]
make man TARGET=coverage
```

## Descripción

Delega en `go.sh --goal coverage` → `go test -race -coverpkg=./internal/... -coverprofile=coverage.out ./...` y falla si la cobertura total queda por debajo del 80 % (`--coverage-min`).

## Ejemplos

```bash
make coverage
bash helpers/shell/go.sh --goal coverage --module source/backend/go/bridge --coverage-min 85
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
