+++
doctype = "convention"
id = "CONV-0002"
title = "Secretos del puente"
status = "active"
created_at = "2026-10-14"
related_architecture = []
related_decisions = ["DEC-0005"]
tags = ["security", "secrets"]
+++

# CONV-0002 - Secretos del puente

## Justificación

Token de administración, clave de invitaciones y claves de emisión dan control total o publican en canales ajenos.

## Regla

Solo por entorno (.env fuera del repo, sops/age en despliegue). Nunca en logs ni en el estado de la emisión: todo lo que el publicador FFmpeg escribe (órdenes, stderr, lastError) pasa por redact(), también en nivel debug. La API devuelve claves enmascaradas (MaskedKey).

## Consecuencias

Revisión de código: cualquier log o respuesta con PublishURL es un bug.
