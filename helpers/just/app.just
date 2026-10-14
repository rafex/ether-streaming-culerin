# app.just — Operativas del puente (desarrollo local y contenedores).
# Todas las recetas reenvían flags explícitos al helper (`--port 8091`, `--man`).

# Puente local en modo desarrollo (`--port 8090`): salas bajo demanda, token admin "dev"
@run *args:
    bash helpers/shell/go.sh --goal run --module {{module}} {{args}}

# Construir la imagen y levantar el puente en un contenedor (`--dev`, `--env-file .env`, `--port 8090`, `--udp-port 8443`)
@up *args:
    bash helpers/shell/container.sh --action app-run --module {{module}} {{args}}

# Detener el contenedor del puente
@down *args:
    bash helpers/shell/container.sh --action app-stop {{args}}

# Seguir los logs del contenedor del puente
@logs *args:
    bash helpers/shell/container.sh --action app-logs {{args}}

# Servidor RTMP local (MediaMTX) para probar la emisión sin YouTube/Twitch
@mediamtx *args:
    bash helpers/shell/container.sh --action mediamtx-up {{args}}

# Detener MediaMTX
@mediamtx-down *args:
    bash helpers/shell/container.sh --action mediamtx-down {{args}}

# Sala de demostración: crea la sala, añade el destino y muestra el enlace (`--room demo`, `--rtmp-url …`)
@demo *args:
    bash helpers/shell/demo.sh {{args}}
