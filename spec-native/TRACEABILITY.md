# TRACEABILITY.md

Mapa de relaciones entre specs, tareas, decisiones y validacion.

## Objetivo

Permitir que una persona o agente pueda reconstruir rapidamente:

- que spec origino un cambio
- que tareas ejecutaron esa spec
- que decisiones condicionaron el trabajo
- que evidencia valida el resultado

## Cuando actualizar este archivo

Actualizar al cerrar una iniciativa, no durante la ejecucion.
El momento correcto es cuando la spec pasa a estado `done` o `blocked`.

Si una decision cambia el alcance de una spec activa, registrar
la relacion antes de continuar.

## Formato sugerido

| Spec | Estado | Tareas | Decisiones | Archivos principales | Validacion | Observaciones |
| --- | --- | --- | --- | --- | --- | --- |
| SPEC-0001 bridge-mvp | done | TASK-0001…TASK-0009 (done) | DEC-0001…DEC-0006, ARCH-0001, ARCH-0002, CONV-0001, CONV-0002 | `source/backend/go/bridge/*`, `containers/*`, `helpers/*` | `make ci` (cobertura 88.8 %), `make e2e` (webrelay 5/5), MediaMTX, emisión a YouTube confirmada, CI de GitHub en verde, despliegue en k3s Server 2 verificado | Cerrada el 2026-10-14. Despliegue: `rafex/ether-streaming-gitops` (streaming.rafex.app). |
