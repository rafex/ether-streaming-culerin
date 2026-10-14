# PRODUCT.md

Fuente de verdad del producto.

## Problema

Los organizadores que invitan gente a una transmisión en vivo (podcast, panel,
clase) necesitan una sala de videollamada propia que se publique en sus
canales (YouTube, Twitch, Facebook, servidor propio) sin depender de un
estudio web cerrado. El webrelay (`ether-webrelay`) resuelve la antesala y la
llamada en el navegador; falta el **puente** que alberga la llamada, compone
la sala y la publica en RTMP(S).

## Usuarios

- **Invitados** (a través del webrelay): entran por enlace, se ven y se oyen.
  No saben nada de RTMP.
- **Anfitrión / producción**: crea salas, reparte invitaciones, configura
  destinos y claves, decide quién está en escena y la composición, sale al
  aire y detiene la emisión, desde la consola o la API.
- **Operación**: despliega el puente en un contenedor con configuración por
  entorno y secretos fuera del repositorio.

## Objetivos

- Implementar el contrato de señalización v2 del webrelay de forma que su E2E
  pase sin cambios contra el puente real.
- SFU WebRTC que reenvía las pistas de cada invitado a los demás.
- Composición en el servidor (mosaico, pantalla en foco, tarjetas con nombre,
  audio mezclado) y publicación simultánea en varios destinos RTMP(S).
- Cambios de composición sin cortar las conexiones RTMP(S).
- API REST + consola web del anfitrión; invitaciones firmadas con caducidad.

### Métricas de éxito

- `make e2e` (Playwright del webrelay contra este puente): 5/5.
- Emisión continua (sin reconexión RTMP) a través de entradas, salidas,
  pantalla compartida y cámaras apagadas; huecos < 5 s.
- Cobertura ≥ 80 %; CI en contenedor verde.

## No objetivos

- Frontend de invitados (vive en `ether-webrelay`).
- Persistencia de salas, multi-instancia y alta disponibilidad (esta etapa).
- Grabación como producto (solo grabación local opcional para depurar).
- Chat, reacciones, simulcast.

## Valor diferencial

Un puente pequeño, autoalojable (un binario Go + FFmpeg), con un contrato de
señalización abierto y probado de extremo a extremo con su cliente.
