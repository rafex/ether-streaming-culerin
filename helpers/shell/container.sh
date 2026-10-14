#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

# ------------------------------------------------------------------
# Helper de contenedores — runtime, imágenes, CI, puente y MediaMTX.
# Reutilizable por Makefile (helpers/mk/container.mk) y Justfile.
#
# Stack (regla 08): Podman primero, Docker con warning; Containerfile, nunca Dockerfile.
#
# Contrato de flags (regla 01):
#   --action runtime|image|ci|app-image|app-run|app-stop|app-logs|mediamtx-up|mediamtx-down
#   --project-name ether-streaming-culerin
#   --container-runtime podman|docker   Sobreescribe la detección
#   --ci-image / --ci-containerfile     Imagen de CI (toolchain Go + FFmpeg)
#   --app-image / --app-containerfile   Imagen del puente
#   --app-name ether-bridge             Nombre del contenedor del puente
#   --port 8090                         Puerto HTTP publicado (solo 127.0.0.1)
#   --udp-port 8443                     Puerto UDP de ICE publicado (solo 127.0.0.1)
#   --env-file .env                     Variables BRIDGE_* (secretos: nunca en el repo)
#   --dev                               BRIDGE_DEV_MODE=true (salas bajo demanda)
#   --version <v>                       Versión incrustada en la imagen
#   --module source/backend/go/bridge
#   --log-file /ruta/al/log  --log-level info  --man
#
# Red: puente y MediaMTX comparten la red "ether"; desde el puente MediaMTX es
# rtmp://ether-mediamtx:1935, desde el host rtmp://localhost:1935.
# Auditoría: /var/log/<proyecto>/log-container-<ts>.log (fallback /tmp/<proyecto>)
# ------------------------------------------------------------------

action=""
container_runtime="${CONTAINER_RUNTIME:-}"
ci_image="ether-bridge-ci:local"
ci_containerfile="containers/ci/Containerfile"
app_image="ether-bridge:local"
app_containerfile="containers/bridge/Containerfile"
app_name="ether-bridge"
port="8090"
udp_port="8443"
env_file=""
dev="false"
version="${VERSION:-dev}"
module="source/backend/go/bridge"
network="ether"
mediamtx_name="ether-mediamtx"
# MediaMTX 1.21.1 (publicada el 2026-09-20), fijada por digest.
mediamtx_image="docker.io/bluenviron/mediamtx:1.21.1@sha256:5ce2a948eb68df06ce2e13870db8df8e30d516ac4dc40e04bfe8aee3bdf7be40"
man="false"
WORKSPACE="${WORKSPACE:-$(pwd)}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--action)            action="${2:-}"; shift 2 ;;
		--project-name)      PROJECT_NAME="${2:-}"; shift 2 ;;
		--container-runtime) container_runtime="${2:-}"; shift 2 ;;
		--ci-image)          ci_image="${2:-}"; shift 2 ;;
		--ci-containerfile)  ci_containerfile="${2:-}"; shift 2 ;;
		--app-image)         app_image="${2:-}"; shift 2 ;;
		--app-containerfile) app_containerfile="${2:-}"; shift 2 ;;
		--app-name)          app_name="${2:-}"; shift 2 ;;
		--port)              port="${2:-}"; shift 2 ;;
		--udp-port)          udp_port="${2:-}"; shift 2 ;;
		--env-file)          env_file="${2:-}"; shift 2 ;;
		--dev)               dev="true"; shift ;;
		--version)           version="${2:-}"; shift 2 ;;
		--module)            module="${2:-}"; shift 2 ;;
		--log-file)          LOG_FILE="${2:-}"; shift 2 ;;
		--log-level)         LOG_LEVEL="${2:-}"; shift 2 ;;
		--man)               man="true"; shift ;;
		*)                   echo "Unknown flag: $1" >&2; exit 1 ;;
	esac
done

if [[ "$man" == "true" ]]; then
	# Las acciones app-* y mediamtx-* se exponen como recetas de just.
	case "$action" in
		app-run)       show_man "up" ;;
		app-stop)      show_man "down" ;;
		app-logs)      show_man "logs" ;;
		mediamtx-up)   show_man "mediamtx" ;;
		mediamtx-down) show_man "mediamtx" ;;
		*)             show_man "${action:-ci}" ;;
	esac
	exit 0
fi
[[ -n "$action" ]] || { echo "Missing required flag --action" >&2; exit 1; }

init_log "container"

resolve_runtime() {
	if [[ -n "$container_runtime" ]]; then
		echo "$container_runtime"
	elif command -v podman >/dev/null 2>&1; then
		echo "podman"
	elif command -v docker >/dev/null 2>&1; then
		log_warn "Docker detectado. Podman es la opción recomendada según la regla 08-stack."
		echo "docker"
	else
		die "No container runtime found. Install podman (preferred) or docker."
	fi
}

runtime="$(resolve_runtime)"
log_info "Container runtime: $runtime"

build_image() {
	local containerfile="$1" image="$2"
	[[ -f "$WORKSPACE/$containerfile" ]] || die "Missing $containerfile"
	log_info "Building $image from $containerfile"
	local format=()
	# Podman produce OCI por defecto, que descarta HEALTHCHECK; el formato docker lo conserva.
	[[ "$runtime" == "podman" ]] && format=(--format docker)
	"$runtime" build "${format[@]}" --build-arg "VERSION=$version" -f "$WORKSPACE/$containerfile" -t "$image" "$WORKSPACE"
}

ensure_network() {
	"$runtime" network exists "$network" >/dev/null 2>&1 || "$runtime" network inspect "$network" >/dev/null 2>&1 ||
		"$runtime" network create "$network" >/dev/null
}

# El árbol entra por stdin (tar) en lugar de un bind-mount: funciona con VMs
# que no comparten la ruta (macOS + volúmenes externos). Solo la caché de Go
# persiste entre ejecuciones (volúmenes con nombre).
run_streamed() {
	local image="$1" command="$2"
	COPYFILE_DISABLE=1 tar -C "$WORKSPACE" \
		--exclude=bin --exclude=coverage.out --exclude=site \
		--exclude=.git --exclude=.specnative/.venv \
		-cf - . |
		"$runtime" run --rm -i \
			-v ether-bridge-gomod:/go/pkg/mod \
			-v ether-bridge-gocache:/root/.cache/go-build \
			-e CI=true \
			"$image" \
			"mkdir -p /workspace && tar -xf - -C /workspace 2>/dev/null && cd /workspace && $command"
}

case "$action" in
	runtime)
		;;
	image)
		build_image "$ci_containerfile" "$ci_image"
		;;
	ci)
		build_image "$ci_containerfile" "$ci_image"
		log_info "Running containerized CI (install + lint + build + coverage)"
		run_streamed "$ci_image" "make install lint build coverage MODULE=$module"
		;;
	app-image)
		build_image "$app_containerfile" "$app_image"
		;;
	app-run)
		build_image "$app_containerfile" "$app_image"
		ensure_network
		"$runtime" rm -f "$app_name" >/dev/null 2>&1 || true
		env_args=(-e "BRIDGE_ICE_PUBLIC_IPS=${BRIDGE_ICE_PUBLIC_IPS:-127.0.0.1}")
		[[ "$dev" == "true" ]] && env_args+=(-e BRIDGE_DEV_MODE=true)
		if [[ -n "$env_file" ]]; then
			[[ -f "$env_file" ]] || die "No existe $env_file"
			env_args+=(--env-file "$env_file")
		fi
		# Solo loopback: el puente se expone detrás de un proxy TLS en producción.
		"$runtime" run -d --name "$app_name" --network "$network" \
			-p "127.0.0.1:$port:8090" -p "127.0.0.1:$udp_port:8443/udp" \
			"${env_args[@]}" "$app_image" >/dev/null
		success "Puente en http://localhost:$port (consola /console/, señalización ws://localhost:$port/signal)"
		;;
	app-stop)
		"$runtime" rm -f "$app_name" >/dev/null 2>&1 || true
		success "Contenedor $app_name detenido."
		;;
	app-logs)
		"$runtime" logs -f "$app_name"
		;;
	mediamtx-up)
		ensure_network
		"$runtime" rm -f "$mediamtx_name" >/dev/null 2>&1 || true
		"$runtime" run -d --name "$mediamtx_name" --network "$network" \
			-p 127.0.0.1:1935:1935 -p 127.0.0.1:8888:8888 "$mediamtx_image" >/dev/null
		success "MediaMTX: publica en rtmp://localhost:1935/<ruta> (desde el puente en contenedor: rtmp://$mediamtx_name:1935/<ruta>); mira en http://localhost:8888/<ruta>"
		;;
	mediamtx-down)
		"$runtime" rm -f "$mediamtx_name" >/dev/null 2>&1 || true
		success "MediaMTX detenido."
		;;
	*)
		die "Unknown action: $action"
		;;
esac
