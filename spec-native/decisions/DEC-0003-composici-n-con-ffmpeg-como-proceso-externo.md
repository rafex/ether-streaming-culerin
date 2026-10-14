+++
doctype = "decision"
id = "DEC-0003"
title = "Composición con FFmpeg como proceso externo"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["ffmpeg", "composition"]
+++

# DEC-0003 - Composición con FFmpeg como proceso externo

## Contexto

La sala debe salir como un solo video con audio mezclado hacia RTMP(S). Alternativas: GStreamer (dependencias nativas y bindings), composición en Go (sin decodificadores VP8/H.264 puros), servicios externos.

## Decisión

El SFU entrega RTP por UDP local a un proceso FFmpeg descrito por SDP; FFmpeg compone (overlay sobre un fondo, drawbox/drawtext, amix) y codifica x264/AAC. Las entradas usan el reloj de pared (-use_wallclock_as_timestamps + -copyts) y el fondo marca el ritmo (realtime).

## Consecuencias

FFmpeg pasa a ser dependencia de runtime (en la imagen). El plan de composición es puro (dominio) y el adaptador lo traduce a un grafo de filtros. Un input detenido congela overlay/amix: se mitiga con el detector de pistas detenidas (tarjeta) y el relevo de ejecuciones.
