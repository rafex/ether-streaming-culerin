# ether-streaming-culerin

El **puente** de Ether: alberga la videollamada a la que entran los invitados
desde [ether-webrelay](https://github.com/rafex/ether-webrelay), compone la
sala (mosaico, pantalla en foco, nombres, audio mezclado) y la **publica en
RTMP(S)** — YouTube, Twitch, Facebook o un servidor propio — a la vez. El
anfitrión lo controla con una API REST y una consola web.

- **Go 1.27 + Pion** (SFU WebRTC) · **FFmpeg** (composición x264/AAC y publicación `tee`)
- Cambios de layout **sin cortar** las conexiones RTMP(S) (empalme FLV)
- Invitaciones firmadas (HMAC) · claves de emisión nunca expuestas
- Implementa la [señalización v2](https://github.com/rafex/ether-webrelay/blob/main/docs/signaling.md) del webrelay; su E2E corre contra este puente

## Inicio rápido

```bash
just setup
just run                 # puente en modo desarrollo: http://localhost:8090/console/ (token "dev")
just mediamtx            # RTMP local para probar la emisión
just demo --live         # sala + destino MediaMTX + invitación, al aire
```

Con el webrelay en `../ether-webrelay` (`just dev`), abre
`http://localhost:5173/join/demo?t=dev` y mira la emisión en
`http://localhost:8888/live/demo`.

```bash
make test                # unit + integración + arquitectura (-race)
make e2e                 # Playwright del webrelay contra este puente
make ci                  # pipeline completo en contenedor
just up --dev            # el puente en un contenedor
```

## Documentación

`make docs` genera el sitio; las fuentes están en [`docs/`](docs/index.md):
[guía de inicio](docs/getting-started.md) ·
[despliegue](docs/running-bridge.md) ·
[configuración](docs/configuration.md) ·
[API](docs/api.md) ·
[arquitectura](docs/architecture.md) ·
[composición](docs/composition.md).

El contexto del proyecto (producto, decisiones, tareas) vive en
[`spec-native/`](spec-native/README.md) — ver [AGENTS.md](AGENTS.md).

## Licencia

[MIT](LICENSE)
