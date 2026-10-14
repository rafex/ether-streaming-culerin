+++
doctype = "architecture"
id = "ARCH-0002"
title = "Tubería de medios: SFU → compositor → publicador"
status = "active"
created_at = "2026-10-14"
related_decisions = ["DEC-0003", "DEC-0004"]
related_specs = ["SPEC-0001"]
tags = ["media", "webrtc", "ffmpeg"]
+++

# ARCH-0002 - Tubería de medios: SFU → compositor → publicador

## Contexto

Los invitados publican por WebRTC; la sala se publica en RTMP(S).

## Diseño

SFU Pion: una source por pista publicada (TrackRemote → TrackLocalStaticRTP compartida por los suscriptores, sin extensiones de cabecera) y taps que copian RTP a UDP local. Broadcaster: recalcula domain.PlanComposition (escena, layout, liveness por LastPacket) en cada cambio y cada segundo; aplica si cambia la firma. Compositor FFmpeg por plan → FLV → empalmador Go → publicador FFmpeg (tee a cada destino).

## Restricciones y consecuencias

Ver docs/architecture.md y docs/composition.md.
