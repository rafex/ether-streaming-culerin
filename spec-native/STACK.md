# STACK.md

Fuente de verdad de la base tecnológica del proyecto.

### Runtime

- Lenguaje: **Go 1.27** (elegido por el usuario; fuera del stack recomendado
  de ether-rules — DEC-0001). Binario estático (`CGO_ENABLED=0`).
- FFmpeg 7.1+ con `libx264`, `aac`, `drawtext` (proceso externo).

### Dependencias

- `github.com/pion/webrtc/v4` v4.2.22 — SFU (ICE, DTLS-SRTP, interceptores NACK/RTCP/TWCC).
- `github.com/coder/websocket` v1.8.15 — WebSocket de señalización.
- Biblioteca estándar para HTTP (`net/http` con patrones de método), JSON,
  HMAC, logging (`log/slog`).
- Política: versiones con ≥ 2 semanas de antigüedad, fijadas en `go.sum`.

### Infraestructura

- Contenedores: Podman (Docker con aviso), `Containerfile`, bases fijadas por
  digest: `golang:1.27.0-trixie` (build/CI), `debian:trixie-20260918-slim`
  (runtime, con FFmpeg + DejaVu).
- Pruebas de emisión: MediaMTX 1.21.1 (fijado por digest).
- CI: GitHub Actions como envoltura de `make ci` y `make e2e`.
- Documentación: Markdown + Mermaid + MkDocs Material.
- Estado: en memoria (sin base de datos).

### Integraciones

- **ether-webrelay** (crítica): señalización v2 por WebSocket + WebRTC. El
  contrato vive en ese repositorio (`docs/signaling.md`).
- **Ingestas RTMP(S)** (crítica para emitir): YouTube, Twitch, Facebook,
  servidores propios.

### Restricciones

- El navegador debe alcanzar el puerto UDP de ICE (un puerto único,
  `BRIDGE_ICE_UDP_PORT`) en la IP anunciada.
- Secretos (`BRIDGE_ADMIN_TOKEN`, `BRIDGE_INVITE_SECRET`, claves de emisión)
  solo por entorno; nunca en el repositorio ni en el cliente.
