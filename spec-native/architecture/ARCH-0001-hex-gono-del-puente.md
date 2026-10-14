+++
doctype = "architecture"
id = "ARCH-0001"
title = "Hexágono del puente"
status = "active"
created_at = "2026-10-14"
related_decisions = ["DEC-0001"]
related_specs = ["SPEC-0001"]
tags = ["architecture", "hexagonal"]
+++

# ARCH-0001 - Hexágono del puente

## Contexto

El puente combina un modelo de salas, un protocolo de señalización, WebRTC, procesos FFmpeg y una API; sin límites claros sería imposible probarlo sin red ni FFmpeg.

## Diseño

source/backend/go/bridge: internal/domain (modelo puro, sin dependencias), internal/ports (interfaces SFU, MediaPeer, MediaTap, Compositor, Connection, Clock), internal/application (Hub, Session, Broadcaster; solo domain+ports), internal/infrastructure (sfu, ffmpeg, httpapi, config, clock), cmd/bridge (composición). tests/architecture verifica las dependencias. El Hub usa un mutex y nunca llama adaptadores bajo él salvo Connection.Send (no bloqueante).

## Restricciones y consecuencias

El núcleo se prueba con dobles (cobertura >90 %). Añadir un adaptador no toca la aplicación.
