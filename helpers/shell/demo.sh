#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

# ------------------------------------------------------------------
# demo.sh — Sala de demostración a través de la API del anfitrión:
# crea la sala (si no existe), añade un destino RTMP y emite una invitación.
#
# Contrato (regla 01):
#   --bridge http://localhost:8090      URL del puente
#   --room demo                         Identificador de la sala
#   --title "Ether demo"                Título
#   --rtmp-url rtmp://localhost:1935/live   Ingesta (MediaMTX local por defecto;
#                                       con el puente en contenedor:
#                                       rtmp://ether-mediamtx:1935/live)
#   --stream-key <clave>                (default: el id de la sala)
#   --live                              Sale al aire al terminar
#   --log-file /ruta/al/log  --log-level info  --man
#
# El token de administración sale de BRIDGE_ADMIN_TOKEN (default "dev",
# el del modo desarrollo). Requiere curl.
# ------------------------------------------------------------------

bridge="http://localhost:8090"
room="demo"
title="Ether demo"
rtmp_url="rtmp://localhost:1935/live"
stream_key=""
live="false"
man="false"
WORKSPACE="${WORKSPACE:-$(pwd)}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--bridge)     bridge="${2:-}"; shift 2 ;;
		--room)       room="${2:-}"; shift 2 ;;
		--title)      title="${2:-}"; shift 2 ;;
		--rtmp-url)   rtmp_url="${2:-}"; shift 2 ;;
		--stream-key) stream_key="${2:-}"; shift 2 ;;
		--live)       live="true"; shift ;;
		--log-file)   LOG_FILE="${2:-}"; shift 2 ;;
		--log-level)  LOG_LEVEL="${2:-}"; shift 2 ;;
		--man)        man="true"; shift ;;
		*)            echo "Unknown flag: $1" >&2; exit 1 ;;
	esac
done
if [[ "$man" == "true" ]]; then show_man "demo"; exit 0; fi
init_log "demo"
stream_key="${stream_key:-$room}"
token="${BRIDGE_ADMIN_TOKEN:-dev}"

api() {
	local method="$1" path="$2" body="${3:-}"
	curl -fsS -X "$method" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
		${body:+--data "$body"} "$bridge/api/v1$path"
}

curl -fsS "$bridge/healthz" >/dev/null || die "El puente no responde en $bridge (just run | just up --dev)"
if ! api GET "/rooms/$room" >/dev/null 2>&1; then
	api POST /rooms "{\"id\":\"$room\",\"title\":\"$title\",\"host\":{\"name\":\"Anfitrión\"}}" >/dev/null
	log_info "Sala $room creada"
fi
if ! api GET "/rooms/$room" | grep -q "\"url\":\"$rtmp_url\""; then
	api POST "/rooms/$room/destinations" "{\"name\":\"MediaMTX local\",\"url\":\"$rtmp_url\",\"streamKey\":\"$stream_key\"}" >/dev/null
	log_info "Destino $rtmp_url/$stream_key añadido"
fi
invite="$(api POST "/rooms/$room/invites" '{"ttlSeconds":86400}' | sed -E 's/.*"url":"([^"]+)".*/\1/')"
if [[ "$live" == "true" ]]; then
	api POST "/rooms/$room/broadcast/start" >/dev/null
	log_info "Sala $room al aire"
fi
success "Invitación: $invite"
echo "  Consola:   $bridge/console/"
echo "  Emisión:   http://localhost:8888/live/$stream_key  (MediaMTX)"
