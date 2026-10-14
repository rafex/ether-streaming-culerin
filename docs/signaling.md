---
title: Señalización
tags: [signaling, webrtc, contract]
---

# Señalización v2 en el puente

El contrato es el del webrelay
([`docs/signaling.md`](https://github.com/rafex/ether-webrelay/blob/main/docs/signaling.md));
aquí solo se documenta cómo lo implementa el puente.

- `GET /signal` exige el subprotocolo `ether-webrelay.signaling.v2` (si no,
  400) y un `Origin` permitido por `BRIDGE_ALLOWED_ORIGINS`. Mensajes de hasta
  256 KiB; un socket sin tráfico durante 30 s se cierra.
- Cada socket es una `application.Session`: `room.peek` → `join` →
  negociación. Los mensajes se procesan en orden; los desconocidos se ignoran
  y los inválidos responden `error` no fatal (`bad-request`).
- `room.peek` valida la invitación (HMAC, sala, caducidad). Los errores
  fatales cierran el socket con un código 4xxx:

| `code` | Cierre |
|--------|--------|
| `invalid-token` | 4401 |
| `kicked` | 4403 |
| `room-not-found` | 4404 |
| `room-closed` | 4410 |
| `room-full` | 4429 |

- `welcome` se envía **antes** de cualquier mensaje del SFU (`tracks`,
  `subscribe.offer`, `ice`) e incluye al propio participante.
- El SFU nunca envía un `subscribe.offer` sin secciones de medios; envía
  `tracks` justo antes de cada oferta y pide un keyframe (PLI) a los emisores
  200 ms después de cada respuesta.
- **Reanudación**: si cae el socket, el participante conserva su sitio y sus
  conexiones WebRTC durante 20 s. `join {resume}` lo recupera (`welcome
  {resumed: true}`) y el puente vuelve a ofrecer `subscribe` (con ICE restart
  si los medios también cayeron). Pasado el plazo sale de la sala.
- Modo desarrollo: salas bajo demanda (`Sala de pruebas · <id>`, anfitrión
  `Anfitrión de pruebas`, canales youtube/twitch), token `invalid` rechazado y
  sala `closed` inexistente — igual que `dev-bridge.mjs` del webrelay.
