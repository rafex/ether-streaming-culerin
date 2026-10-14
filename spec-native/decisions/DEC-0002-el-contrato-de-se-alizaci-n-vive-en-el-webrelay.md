+++
doctype = "decision"
id = "DEC-0002"
title = "El contrato de señalización vive en el webrelay"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["contract", "signaling", "e2e"]
+++

# DEC-0002 - El contrato de señalización vive en el webrelay

## Contexto

El webrelay definió la señalización v2 (docs/signaling.md) y la valida con su puente de desarrollo y un E2E.

## Decisión

Este puente implementa ese contrato sin extenderlo unilateralmente. Cualquier cambio se propone en ether-webrelay (CONV-0006 de ese repo) y se verifica con su E2E ejecutado contra este puente (make e2e). El modo desarrollo reproduce los ganchos del puente de pruebas (token invalid, sala closed, salas bajo demanda).

## Consecuencias

El E2E del webrelay es la prueba de aceptación del puente. El job e2e de CI clona el webrelay (main).
