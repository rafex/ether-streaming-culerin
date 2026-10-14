+++
doctype = "decision"
id = "DEC-0001"
title = "Go + Pion para el puente"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["stack", "go", "webrtc"]
+++

# DEC-0001 - Go + Pion para el puente

## Contexto

El usuario eligió el stack del puente. ether-rules recomienda Java/Python/Node/Rust y su scaffold no cubre Go.

## Decisión

El puente se escribe en Go 1.27 con pion/webrtc v4 como SFU y la biblioteca estándar para HTTP/JSON/HMAC/logging. Se siguen igualmente las reglas de ether-rules (estructura source/backend/go/bridge, make/just/helpers, Containerfile, docs-as-code).

## Consecuencias

Un binario estático pequeño y un SFU sin dependencias nativas. Los helpers Go (go.sh, go.mk) son propios de este repo. Nuevas dependencias deben justificarse frente a la biblioteca estándar.
