# app-image

## Nombre

app-image — imagen del puente

## Sinopsis

```
make app-image [VERSION=<v>]
make man TARGET=app-image
```

## Descripción

Construye `containers/bridge/Containerfile`: compilación en `golang:1.27.0-trixie` y ejecución en `debian:trixie-slim` con FFmpeg y DejaVu, usuario sin privilegios (10001), `HEALTHCHECK` sobre `/healthz`. Expone `8090/tcp` (HTTP) y `8443/udp` (ICE).

## Ejemplos

```bash
make app-image
make app-image VERSION=0.1.0
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
