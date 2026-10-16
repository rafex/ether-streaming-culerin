---
title: API del anfitrión
tags: [api, host, contract]
---

# API del anfitrión

REST + JSON en `/api/v1`, autenticada con `Authorization: Bearer <BRIDGE_ADMIN_TOKEN>`.
La consola (`/console/`) es un cliente de esta API. Errores:
`{"code": "...", "message": "..."}` con 400 (`bad-request`), 401
(`unauthorized`), 404 (`not-found`), 409 (`conflict`, `no-destinations`,
`room-closed`) o 502 (`media-error`). Las claves de emisión **nunca** se
devuelven completas.

| Método y ruta | Cuerpo | Resultado |
|---------------|--------|-----------|
| `GET /config` | — | `{devMode, webrelayUrl, output}` |
| `GET /rooms` | — | `{rooms: [Room]}` (más recientes primero) |
| `POST /rooms` | `{id?, title, host: {name, avatar?}, capacity?, layout?}` | 201 `Room` (id aleatorio si se omite) |
| `GET /rooms/{room}` | — | `Room` |
| `PATCH /rooms/{room}` | `{title?, host?: {name?, avatar?}, capacity?, layout?}` | `Room` |
| `DELETE /rooms/{room}` | — | 204; cierra la sala: expulsa (`room-closed`) e invalida las invitaciones |
| `POST /rooms/{room}/invites` | `{ttlSeconds?, maxUses?}` | 201 `{id, token, url, expiresAt, maxUses}`. `maxUses` 0–1000 (0: sin límite) |
| `GET /rooms/{room}/invites` | — | 200 lista `{id, createdAt, expiresAt, maxUses, uses, revoked, expired, active}`, la más nueva primero. **Nunca incluye el token ni el enlace** |
| `DELETE /rooms/{room}/invites/{id}` | — | 204; revoca esa invitación (404 si no existe) |
| `POST /rooms/{room}/destinations` | `{name, url, streamKey, kind?, enabled?}` | 201 `Destination` |
| `PATCH /rooms/{room}/destinations/{id}` | `{enabled}` | `Room` |
| `DELETE /rooms/{room}/destinations/{id}` | — | 204 |
| `POST /rooms/{room}/broadcast/start` | — | `Room` (409 sin destinos activos) |
| `POST /rooms/{room}/broadcast/stop` | — | `Room` |
| `GET /rooms/{room}/layout` | — | `CompositionPlan` actual (también fuera del aire) |
| `PATCH /rooms/{room}/participants/{id}` | `{onStage}` | `Room` |
| `DELETE /rooms/{room}/participants/{id}` | — | 204; el invitado recibe `kicked` |

Públicos: `GET /healthz` y `GET /signal` (señalización v2).

## Tipos

```json
{
  "id": "demo", "title": "Podcast", "host": {"name": "Rafex"},
  "capacity": 8, "layout": "auto", "closed": false, "ephemeral": false,
  "createdAt": "2026-10-13T17:00:00Z", "channels": ["youtube"],
  "participants": [{
    "id": "p-1", "name": "Ana", "title": "Invitada", "muted": false,
    "cameraOff": false, "screenSharing": false, "onStage": true,
    "connected": true, "published": ["camera", "microphone"],
    "joinedAt": "2026-10-13T17:01:00Z"
  }],
  "destinations": [{
    "id": "d-1", "name": "YouTube", "kind": "youtube",
    "url": "rtmps://a.rtmp.youtube.com/live2", "streamKey": "••••••••9f3c", "enabled": true
  }],
  "broadcast": {"live": true, "since": "2026-10-13T17:02:00Z", "restarts": 0}
}
```

- `layout`: `auto` (mosaico; la pantalla compartida en foco con las cámaras en
  columna), `grid` (siempre mosaico), `speaker` (el primero en escena en foco).
- `kind`: `youtube`, `twitch`, `facebook`, `custom` (se deduce del host si se
  omite). Es lo que el lobby muestra como "canales".
- Una invitación es un token firmado (HMAC) que lleva la sala, la caducidad y el
  **id** de un registro que el puente guarda en memoria. La firma prueba que el
  puente la emitió; que siga valiendo lo decide el registro: no revocada, no
  caducada y con usos libres (`maxUses`). El token y el enlace solo se devuelven
  al crearla; el puente no puede volver a mostrarlos, solo listar su estado y
  revocarla.
- Un uso se cuenta cuando un invitado **consigue sitio** (no al abrir el lobby,
  no al reanudar tras una caída, no si la sala está llena). Revocar o agotar una
  invitación no expulsa a quien ya entró (para eso `DELETE …/participants/{id}`)
  pero sí impide entrar a quien tenía el lobby abierto y aún no había pulsado
  «Entrar».
- Hasta 100 invitaciones activas por sala (las caducadas o revocadas no cuentan).
  Los tokens del formato anterior (`v1`, sin id) ya no se aceptan; el estado es
  en memoria, así que un reinicio los habría invalidado de todos modos.

## Ejemplo

```bash
T="Authorization: Bearer $BRIDGE_ADMIN_TOKEN"
curl -sH "$T" -X POST localhost:8090/api/v1/rooms -d '{"id":"podcast","title":"Podcast","host":{"name":"Rafex"}}'
curl -sH "$T" -X POST localhost:8090/api/v1/rooms/podcast/destinations \
  -d '{"name":"YouTube","url":"rtmps://a.rtmp.youtube.com/live2","streamKey":"xxxx-xxxx"}'
curl -sH "$T" -X POST localhost:8090/api/v1/rooms/podcast/invites -d '{"ttlSeconds":7200}'
curl -sH "$T" -X POST localhost:8090/api/v1/rooms/podcast/broadcast/start
```
