+++
doctype = "convention"
id = "CONV-0001"
title = "Cambios de contrato con el webrelay"
status = "active"
created_at = "2026-10-14"
related_architecture = []
related_decisions = ["DEC-0002"]
tags = ["contract", "signaling"]
+++

# CONV-0001 - Cambios de contrato con el webrelay

## Justificación

El puente y el webrelay evolucionan en repositorios distintos.

## Regla

Ningún mensaje, campo o código de error nuevo en /signal sin actualizar primero docs/signaling.md de ether-webrelay y su parser; el puente acepta y emite solo lo que el contrato define. make e2e debe pasar antes de cerrar cualquier tarea que toque señalización o SFU.

## Consecuencias

Los dos repos se mantienen compatibles; el E2E detecta divergencias.
