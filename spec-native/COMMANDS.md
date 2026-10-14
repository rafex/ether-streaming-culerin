# COMMANDS.md

Comandos operativos del proyecto. Todos delegan en `helpers/shell/*.sh`;
cada uno tiene manual: `make man TARGET=<tarea>` o `just <receta> --man`.

## Setup

```bash
just setup                 # git hooks (.githooks) + go mod download
make install               # go mod download + verify
```

## Desarrollo

```bash
just run                   # puente en modo desarrollo: http://localhost:8090 (consola /console/, token "dev")
just mediamtx              # RTMP local (1935) + HLS (8888) para ver la emisión
just demo --live           # sala "demo" + destino MediaMTX + invitación, al aire
just up --dev              # imagen + contenedor (--env-file .env fuera de desarrollo; --port, --udp-port)
just logs
just down
just mediamtx-down
```

## Tests

```bash
make test                  # todo con -race
make test-unit             # dominio + aplicación + arquitectura
make test-integration      # Pion, FFmpeg, HTTP/WebSocket
make coverage              # umbral 80 %
make e2e                   # Playwright del webrelay (../ether-webrelay) contra este puente
```

## Lint y formato

```bash
make lint                  # gofmt -l + go vet (+ staticcheck si existe)
make format                # gofmt -w
```

## Build

```bash
make build                 # source/backend/go/bridge/bin/bridge (VERSION=…)
make app-image             # imagen ether-bridge:local
make clean
```

## CI

```bash
make ci                    # install + lint + build + coverage en contenedor
make docs                  # sitio MkDocs en site/
```
