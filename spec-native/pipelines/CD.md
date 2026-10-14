# CD.md

Cómo llega el código a producción.

### Estado

Sin despliegue automatizado en esta etapa. Artefacto: la imagen
`containers/bridge/Containerfile` (`make app-image VERSION=<v>`).

### Despliegue manual

1. `make app-image VERSION=<v>` y publicar la imagen en el registro elegido.
2. En el host: `.env` con `BRIDGE_ADMIN_TOKEN`, `BRIDGE_INVITE_SECRET`,
   `BRIDGE_WEBRELAY_URL`, `BRIDGE_ALLOWED_ORIGINS`, `BRIDGE_ICE_PUBLIC_IPS`
   (secretos por sops/age según la regla 13; nunca en el repo).
3. Contenedor con `8090/tcp` detrás de un proxy TLS y `8443/udp` abierto
   (`docs/running-bridge.md`).
4. Verificar `GET /healthz` y una sala de prueba con `just demo --bridge https://…`.

### Rollback

Volver a la imagen anterior; el estado es en memoria, así que un reinicio
cierra las salas en curso (avisar al anfitrión).
