# test-unit

## Nombre

test-unit — tests del núcleo (dominio, aplicación, arquitectura)

## Sinopsis

```
make test-unit
make man TARGET=test-unit
```

## Descripción

Delega en `go.sh --goal test-unit`: `internal/domain`, `internal/application` (con dobles de SFU, compositor y reloj) y `tests/architecture` (regla de dependencias del hexágono). No necesitan red ni FFmpeg.

## Ejemplos

```bash
make test-unit
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
