---
title: Desplegar el puente
tags: [deployment, container]
---

# Desplegar el puente

## Contenedor

```bash
cp .env.example .env          # rellena BRIDGE_ADMIN_TOKEN y BRIDGE_INVITE_SECRET
just up --env-file .env       # o: just up --dev  (solo en tu máquina)
just logs
```

La imagen (`containers/bridge/Containerfile`) lleva el binario estático, FFmpeg
y DejaVu; corre sin privilegios (uid 10001) y expone:

| Puerto | Uso |
|--------|-----|
| `8090/tcp` | señalización (`/signal`), API (`/api/v1`), consola (`/console/`), `/healthz` |
| `8443/udp` | todo el tráfico ICE/SRTP de los invitados (un solo puerto) |

## Red y WebRTC

El navegador debe alcanzar el puerto UDP del SFU en la dirección que el puente
anuncia en sus candidatos:

- **Local con contenedor**: `just up` publica `127.0.0.1:8443/udp` y anuncia
  `BRIDGE_ICE_PUBLIC_IPS=127.0.0.1`.
- **Servidor con IP pública**: `BRIDGE_ICE_PUBLIC_IPS=<ip pública>` y abre
  `8443/udp` en el firewall.
- **Detrás de NAT sin 1:1**: añade un TURN en `BRIDGE_ICE_SERVERS`.

## TLS y orígenes

Los navegadores solo capturan cámara en contextos seguros, así que el webrelay
irá por HTTPS y su señalización por `wss://`. Termina TLS en un proxy delante
del puerto 8090 (reenviando el upgrade de WebSocket) y fija
`BRIDGE_ALLOWED_ORIGINS` al host del webrelay.

```nginx
location /signal {
    proxy_pass http://127.0.0.1:8090;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 60s;
}
location / { proxy_pass http://127.0.0.1:8090; }
```

Restringe `/api/` y `/console/` (VPN, lista de IPs o autenticación del proxy):
el token de administración es la única barrera de la API.

## Salud y operación

- `GET /healthz` → `{"status":"ok"}` (usado por el `HEALTHCHECK`).
- Logs estructurados en stderr (`BRIDGE_LOG_LEVEL=debug` muestra las órdenes
  y la salida de FFmpeg).
- El estado (salas, destinos) vive en memoria: un reinicio cierra las salas.
- `SIGTERM` cierra la emisión limpiamente (EOF al publicador) antes de salir.
