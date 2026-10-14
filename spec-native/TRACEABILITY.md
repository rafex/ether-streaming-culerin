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
| SPEC-0001 bridge-mvp | active | TASK-0001…TASK-0008 (done), TASK-0009 (todo) | DEC-0001…DEC-0006, ARCH-0001, ARCH-0002, CONV-0001, CONV-0002 | `source/backend/go/bridge/*`, `containers/*`, `helpers/*` | `make ci`, `make e2e`, MediaMTX | Falta la prueba con un destino real; el webrelay cierra su TASK-0110 con `make e2e` |
