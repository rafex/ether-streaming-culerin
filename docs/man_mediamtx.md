# mediamtx

## Nombre

mediamtx — servidor RTMP local para pruebas

## Sinopsis

```
just mediamtx
just mediamtx-down
make man TARGET=mediamtx
```

## Descripción

Levanta MediaMTX 1.21.1 (fijado por digest) en la red `ether`, publicado en `127.0.0.1:1935` (RTMP) y `127.0.0.1:8888` (HLS para mirar la emisión en el navegador). Desde un puente en el host el destino es `rtmp://localhost:1935/live` + clave; desde el puente en contenedor, `rtmp://ether-mediamtx:1935/live`.

## Ejemplos

```bash
just mediamtx
just demo --live
open http://localhost:8888/live/demo
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
