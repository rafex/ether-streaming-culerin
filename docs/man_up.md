# up

## Nombre

up — levanta el puente en un contenedor

## Sinopsis

```
just up [--dev] [--env-file .env] [--port 8090] [--udp-port 8443] [--man]
make man TARGET=up
```

## Descripción

Construye la imagen del puente y la ejecuta en la red `ether`, publicada solo en `127.0.0.1` (HTTP en `--port`, ICE en `--udp-port`/udp). Anuncia `BRIDGE_ICE_PUBLIC_IPS` (por defecto `127.0.0.1`) para que el navegador del host alcance el SFU. Fuera de `--dev` hacen falta `BRIDGE_ADMIN_TOKEN` y `BRIDGE_INVITE_SECRET` (por `--env-file`). En producción pon un proxy TLS delante del HTTP y publica el puerto UDP con la IP pública.

## Ejemplos

```bash
just up --dev
just up --env-file .env
just down
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
