# test-integration

## Nombre

test-integration — tests de los adaptadores

## Sinopsis

```
make test-integration
make man TARGET=test-integration
```

## Descripción

Delega en `go.sh --goal test-integration`: SFU con clientes Pion reales, compositor y empalmador con FFmpeg real, API REST y señalización por WebSocket con `httptest`.

## Ejemplos

```bash
make test-integration
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
