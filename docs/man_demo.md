# demo

## Nombre

demo — sala de demostración por la API

## Sinopsis

```
just demo [--bridge http://localhost:8090] [--room demo] [--title T] [--rtmp-url URL] [--stream-key K] [--live]
make man TARGET=demo
```

## Descripción

Usa la API del anfitrión (token en `BRIDGE_ADMIN_TOKEN`, `dev` por defecto) para crear la sala si no existe, añadir el destino RTMP (MediaMTX local por defecto) y emitir una invitación de 24 h, que imprime. Con `--live` sale al aire.

## Ejemplos

```bash
just run   # en otra terminal
just mediamtx
just demo --live
just demo --rtmp-url rtmp://ether-mediamtx:1935/live   # puente en contenedor
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
