# test

## Nombre

test — todos los tests con el detector de carreras

## Sinopsis

```
make test
make man TARGET=test
```

## Descripción

Delega en `go.sh --goal test` → `go test -race ./...`: dominio, aplicación, arquitectura y adaptadores (Pion, FFmpeg, HTTP/WebSocket). Las pruebas de FFmpeg se saltan si no está instalado.

## Ejemplos

```bash
make test
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
