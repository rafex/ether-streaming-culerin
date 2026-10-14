# lint

## Nombre

lint — formato y análisis estático

## Sinopsis

```
make lint
make man TARGET=lint
```

## Descripción

Delega en `go.sh --goal lint`: falla si `gofmt -l` lista archivos, ejecuta `go vet ./...` y `staticcheck ./...` si está instalado.

## Ejemplos

```bash
make lint
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
