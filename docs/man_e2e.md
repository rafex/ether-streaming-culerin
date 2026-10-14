# e2e

## Nombre

e2e — E2E del webrelay contra este puente

## Sinopsis

```
make e2e
bash helpers/shell/go.sh --goal e2e --module source/backend/go/bridge [--port 18190] [--webrelay-dir <ruta>]
make man TARGET=e2e
```

## Descripción

Compila el puente, lo arranca en modo desarrollo en `127.0.0.1:18190` y ejecuta el Playwright del webrelay (`../ether-webrelay/source/frontend/nodejs/webrelay` por defecto) con `E2E_BRIDGE_URL` apuntando a él: enlace inválido, invitación rechazada, lobby, dos participantes que se ven y se oyen, pantalla compartida. Usa Google Chrome si está instalado. Requiere Node 24 y FFmpeg.

## Ejemplos

```bash
make e2e
bash helpers/shell/go.sh --goal e2e --module source/backend/go/bridge --webrelay-dir ~/src/ether-webrelay/source/frontend/nodejs/webrelay
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
