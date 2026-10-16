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
| `BRIDGE_ADMIN_TOKEN` | — (`dev` en modo desarrollo) | Bearer de la API y la consola; ≥ 32 caracteres (`openssl rand -base64 32`) |
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
| `BRIDGE_TRUSTED_PROXY_HOPS` | `0` | Proxies inversos delante del puente que añaden al cliente a `X-Forwarded-For` (0–5). Solo decide a qué dirección se imputan los límites (ver *Endurecimiento*) |
| `BRIDGE_MAX_ROOMS` | `50` | Tope de salas abiertas (también las bajo demanda del modo desarrollo) |
| `BRIDGE_MAX_DESTINATIONS` | `8` | Tope de destinos RTMP(S) por sala |
| `BRIDGE_ALLOW_PRIVATE_DESTINATIONS` | `true` en modo desarrollo, `false` si no | Permite destinos en loopback/red privada (sumideros RTMP locales). En producción déjalo en `false` |

Ejemplo de perfil más ligero para muchas personas:

```bash
BRIDGE_PROFILE='{"width":960,"height":540,"videoBitrate":900000}'
```

## Endurecimiento

Lo que el puente hace por sí mismo (además de las políticas de red del despliegue):

| Superficie | Medida |
|------------|--------|
| API del anfitrión | Bearer comparado en tiempo constante. Tras 10 intentos fallidos desde una misma dirección responde `429` (+ `Retry-After`) también a un token correcto hasta que se recupera un intento (1 cada 6 s). Cuerpos de hasta 64 KiB y campos desconocidos rechazados |
| `/signal` | `Origin` obligatorio si el cliente lo envía (los navegadores siempre); 20 sockets nuevos de ráfaga y 1/s por dirección, 30 abiertos a la vez; un socket que no presenta una invitación válida (`room.peek`) en 10 s se cierra con 4408; presupuesto de 200 mensajes de ráfaga y 100/s por socket (si no, cierre 1008); mensajes ≤ 256 KiB |
| Sesiones | `join {resume}` exige el `resumeToken` (ver [señalización](signaling.md)); los ids de participante no autentican |
| Destinos | Solo `rtmp://` / `rtmps://` con host público: se rechazan IPs de loopback, privadas, link-local, CGNAT, `localhost` y nombres internos (`.svc`, `.cluster.local`, `.local`…). No ve a través de DNS: un nombre público que resuelva a una IP privada lo frena la `NetworkPolicy` de salida del despliegue |
| Recursos | Máximo de salas y de destinos por sala; invitaciones de hasta 30 días |
| Servidor HTTP | `ReadHeaderTimeout` 10 s, `IdleTimeout` 120 s, cabeceras ≤ 16 KiB. Sin `ReadTimeout`/`WriteTimeout` a propósito: cortarían los WebSockets |
| Cabeceras | `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener/Resource-Policy: same-origin` en todo; `Strict-Transport-Security` siempre (el proxy que termina TLS no avisa al puente, y los navegadores lo ignoran sobre HTTP); la consola añade CSP y `Permissions-Policy` sin cámara ni micrófono |

**Detrás de proxies (obligatorio fijarlo).** El puente solo ve la IP del
último salto. Con el valor por defecto (`BRIDGE_TRUSTED_PROXY_HOPS=0`) todos los
clientes que llegan por un proxy comparten una misma dirección y, por tanto, un
mismo cubo de límites: uno solo que abuse bloquearía a los demás. En un
despliegue tras un ingress pon `N` = número de proxies que añaden al cliente a
`X-Forwarded-For`; el puente usa la entrada N-ésima desde la derecha (lo que
añadió el proxy más externo de confianza; lo que el cliente escriba a la
izquierda se ignora). Si con `N > 0` no llega una entrada utilizable, **los
límites no se aplican** (en vez de imputarse al proxy) y se avisa en el log como
máximo una vez por minuto (`no client address in X-Forwarded-For`): ese aviso
indica un `N` equivocado. Comprueba el valor con un intento de token erróneo
desde tu IP y mira `client=` en el log antes de fiarte de él.
