---
title: Configuración
tags: [configuration, security]
---

# Configuración

Todo se configura con variables de entorno `BRIDGE_*` (o `.env` con
`just up --env-file`). Los secretos nunca se versionan (regla 13).

| Variable | Defecto | Descripción |
|----------|---------|-------------|
| `BRIDGE_ADDR` | `:8090` | Dirección HTTP |
| `BRIDGE_DEV_MODE` | `false` | Salas bajo demanda, cualquier token salvo `invalid`, sala `closed` inexistente (como el puente de pruebas del webrelay). **Solo local.** |
| `BRIDGE_ADMIN_TOKEN` | — (`dev` en modo desarrollo) | Bearer de la API y la consola; ≥ 16 caracteres |
| `BRIDGE_INVITE_SECRET` | — (aleatorio en modo desarrollo) | Clave HMAC-SHA256 de las invitaciones; ≥ 32 caracteres |
| `BRIDGE_INVITE_TTL` | `24h` | Validez por defecto de las invitaciones (máx. 30 días) |
| `BRIDGE_WEBRELAY_URL` | `http://localhost:5173` | Base de los enlaces `/join/<sala>?t=…` |
| `BRIDGE_ALLOWED_ORIGINS` | `localhost:*,127.0.0.1:*` | Orígenes aceptados en `/signal` (patrones de host) |
| `BRIDGE_ICE_UDP_PORT` | `0` (efímeros; `8443` en la imagen) | Puerto UDP único para ICE |
| `BRIDGE_ICE_PUBLIC_IPS` | — | IPs anunciadas en lugar de las locales (NAT 1:1, contenedores) |
| `BRIDGE_ICE_LOOPBACK` | `false` | Anunciar candidatos 127.0.0.1 (pruebas en la misma máquina) |
| `BRIDGE_ICE_SERVERS` | `[]` | JSON con STUN/TURN que reciben los invitados en `welcome` |
| `BRIDGE_PROFILE` | 1280×720@30, 1.5 Mbps | JSON parcial del perfil que publican los invitados (`welcome.profile`) |
| `BRIDGE_OUTPUT` | 1280×720@30, 3.5 Mbps, AAC 160k | JSON parcial de la codificación de la emisión |
| `BRIDGE_FFMPEG` | `ffmpeg` | Ruta del binario |
| `BRIDGE_FONT_FILE` | autodetección (DejaVu, Arial) | Fuente de los rótulos; sin fuente no hay nombres |
| `BRIDGE_WORK_DIR` | temporal del sistema | SDP y rótulos de cada composición |
| `BRIDGE_RTP_PORT_MIN` / `_MAX` | `40000` / `40999` | Puertos locales SFU → FFmpeg |
| `BRIDGE_RECORD_DIR` | — | Si se define, cada emisión se graba además en `<dir>/<sala>-<fecha>.flv` |
| `BRIDGE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Ejemplo de perfil más ligero para muchas personas:

```bash
BRIDGE_PROFILE='{"width":960,"height":540,"videoBitrate":900000}'
```
