+++
doctype = "decision"
id = "DEC-0004"
title = "Una ejecución de FFmpeg por layout con empalme FLV sin cortes"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["ffmpeg", "rtmp", "splicing"]
+++

# DEC-0004 - Una ejecución de FFmpeg por layout con empalme FLV sin cortes

## Contexto

FFmpeg no admite añadir o quitar entradas en caliente y reiniciar el proceso que publica reabre las conexiones RTMP(S) (las ingestas cortan o muestran buffering).

## Decisión

Dos procesos: un compositor por plan de composición y un publicador de larga vida (-c copy -f tee, onfail=ignore). Un empalmador en Go lee el FLV del compositor y lo escribe en el publicador reescribiendo marcas de tiempo sobre una línea temporal común. Make-before-break: la ejecución nueva toma el aire con su primer tag; la anterior se mata. Propiedades de color fijas para un SPS idéntico entre ejecuciones.

## Consecuencias

Las conexiones RTMP(S) sobreviven a cualquier cambio de layout; solo un cambio de destinos reinicia el flujo. Durante el relevo hay dos FFmpeg (más CPU unos segundos). Hueco de 2.5–4 s si la ejecución en curso estaba congelada por una entrada detenida.
