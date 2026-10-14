# install

## Nombre

install — descarga y verifica los módulos Go

## Sinopsis

```
make install [MODULE=<ruta>]
make man TARGET=install
```

## Descripción

Delega en `go.sh --goal install` → `go mod download && go mod verify` dentro de `MODULE` (por defecto `source/backend/go/bridge`). Las versiones están fijadas en `go.mod`/`go.sum`.

## Ejemplos

```bash
make install
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
