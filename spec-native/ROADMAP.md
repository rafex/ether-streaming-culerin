# ROADMAP.md

Dirección del proyecto en el tiempo.

## Ahora

- **bridge-mvp** (`SPEC-0001`): cerrada el 2026-10-14. Desplegada en k3s Server 2 (`streaming.rafex.app`) con `rafex/ether-streaming-gitops`.

## Después

- Persistencia de salas y destinos (claves cifradas) para sobrevivir reinicios.
- Reducir el hueco al apagar una cámara (relleno de entradas en Go o
  compositor que no espere entradas detenidas).
- Vista previa de la emisión en la consola (HLS/WebRTC desde el compositor).
- Notificar a los invitados cambios de sala (título) y "en escena".

## Más adelante

- Varias instancias (salas repartidas) y métricas Prometheus.
- Simulcast y adaptación por suscriptor.
- Overlays/marcas de la producción (logos, rótulos) configurables.

## No hacer por ahora

- Frontend de invitados aquí (vive en `ether-webrelay`).
- Que el cliente conozca destinos o claves RTMP(S).
- SDK de SFU de terceros (mediasoup, LiveKit): Pion basta y mantiene un binario.
