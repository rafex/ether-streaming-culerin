+++
doctype = "decision"
id = "DEC-0005"
title = "Invitaciones HMAC sin estado y administración por token"
status = "accepted"
created_at = "2026-10-14"
owners = []
related_specs = ["SPEC-0001"]
related_tasks = []
related_architecture = []
supersedes = []
tags = ["security", "invites", "api"]
+++

# DEC-0005 - Invitaciones HMAC sin estado y administración por token

## Contexto

El webrelay entra con /join/<sala>?t=<token> y el anfitrión necesita repartir enlaces; no hay base de datos.

## Decisión

Token = base64url(v1.<sala>.<caducidad>) + '.' + base64url(HMAC-SHA256). Clave BRIDGE_INVITE_SECRET (≥32 caracteres). La API y la consola usan un bearer BRIDGE_ADMIN_TOKEN comparado en tiempo constante. Fuera del modo desarrollo ambos son obligatorios.

## Consecuencias

Revocar = cerrar la sala o rotar la clave (invalida todas). Las claves de emisión se guardan en memoria y solo se devuelven enmascaradas; nunca llegan al webrelay.
