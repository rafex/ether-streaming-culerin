# image

## Nombre

image — imagen de CI

## Sinopsis

```
make image
make man TARGET=image
```

## Descripción

Construye `containers/ci/Containerfile` (Go 1.27 + FFmpeg + DejaVu, base fijada por digest) como `ether-bridge-ci:local`.

## Ejemplos

```bash
make image
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
