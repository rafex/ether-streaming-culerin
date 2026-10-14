# build

## Nombre

build — binario estático del puente

## Sinopsis

```
make build [MODULE=<ruta>] [VERSION=<v>]
make man TARGET=build
```

## Descripción

Delega en `go.sh --goal build` → `CGO_ENABLED=0 go build -trimpath` de `./cmd/bridge` con la versión incrustada (`bridge --version`). Salida: `MODULE/bin/bridge`. El binario necesita `ffmpeg` en el `PATH` solo para emitir.

## Ejemplos

```bash
make build
make build VERSION=0.1.0
source/backend/go/bridge/bin/bridge --version
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
