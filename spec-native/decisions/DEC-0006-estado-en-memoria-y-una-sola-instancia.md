+++
doctype = "decision"
id = "DEC-0006"
title = "Estado en memoria y una sola instancia"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["state", "scalability"]
+++

# DEC-0006 - Estado en memoria y una sola instancia

## Contexto

MVP para probar con el webrelay y emitir; la persistencia y el escalado añaden infraestructura (BD, coordinación de SFU).

## Decisión

Salas, participantes y destinos viven en memoria en un proceso. Un reinicio cierra las salas.

## Consecuencias

Despliegue trivial (un contenedor). La persistencia (con claves cifradas) y multi-instancia quedan en ROADMAP.
