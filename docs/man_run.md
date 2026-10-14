# run

## Nombre

run — puente local en modo desarrollo

## Sinopsis

```
just run [--port 8090] [--man]
make man TARGET=run
```

## Descripción

Delega en `go.sh --goal run` → `go run ./cmd/bridge` con `BRIDGE_DEV_MODE=true` y `BRIDGE_ADDR=127.0.0.1:<port>` (sobrescribibles por entorno o `.env`). En modo desarrollo las salas se crean al primer `room.peek`, cualquier token sirve salvo `invalid`, y el token de administración es `dev`. Consola: `http://localhost:<port>/console/`. Nunca expongas un puente en modo desarrollo.

## Ejemplos

```bash
just run
just run --port 8091
BRIDGE_LOG_LEVEL=debug just run
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
