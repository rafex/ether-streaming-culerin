#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/logs.sh"
source "$SCRIPT_DIR/lib/messages.sh"

# ------------------------------------------------------------------
# Helper de Go — install, build, test, cobertura, lint, run y E2E.
# Reutilizable por Makefile (helpers/mk/go.mk) y Justfile.
#
# Contrato de flags (regla 01):
#   --goal <goal>                       install | build | test | test-unit |
#                                       test-integration | coverage | lint |
#                                       format | run | e2e
#   --module source/backend/go/bridge   Proyecto (default: .)
#   --port 8090                         Puerto HTTP (run: 8090, e2e: 18190)
#   --webrelay-dir <ruta>               Frontend para el E2E (o WEBRELAY_DIR)
#                                       (default: ../ether-webrelay/source/frontend/nodejs/webrelay)
#   --coverage-min 80                   Umbral de cobertura (coverage)
#   --version <v>                       Versión incrustada en el binario (build)
#   --command "<cmd>"                   Comando directo (sobreescribe goal)
#   --log-file /ruta/al/log  --log-level info  --man
#
# Auditoría: /var/log/<proyecto>/log-go-<ts>.log (fallback /tmp/<proyecto>)
# ------------------------------------------------------------------

goal=""
module="."
port=""
webrelay_dir="${WEBRELAY_DIR:-}"
coverage_min="80"
version="${VERSION:-dev}"
command_line=""
man="false"
WORKSPACE="${WORKSPACE:-$(pwd)}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--goal)          goal="${2:-}"; shift 2 ;;
		--module)        module="${2:-}"; shift 2 ;;
		--port)          port="${2:-}"; shift 2 ;;
		--webrelay-dir)  webrelay_dir="${2:-}"; shift 2 ;;
		--coverage-min)  coverage_min="${2:-}"; shift 2 ;;
		--version)       version="${2:-}"; shift 2 ;;
		--command)       command_line="${2:-}"; shift 2 ;;
		--log-file)      LOG_FILE="${2:-}"; shift 2 ;;
		--log-level)     LOG_LEVEL="${2:-}"; shift 2 ;;
		--man)           man="true"; shift ;;
		*)               echo "Unknown flag: $1" >&2; exit 1 ;;
	esac
done

# --man se resuelve antes de init_log (regla 01): mostrar un manual no se audita.
if [[ "$man" == "true" ]]; then
	show_man "${goal:-go}"
	exit 0
fi
if [[ -z "$goal" && -z "$command_line" ]]; then
	echo "Missing required flag --goal or --command" >&2
	exit 1
fi

init_log "go"
log_info "Goal: ${goal:-<command>} | Module: $module"
command -v go >/dev/null 2>&1 || die "Go no está instalado (ver spec-native/STACK.md)."
cd "$WORKSPACE/$module"

coverage() {
	go test -race -coverpkg=./internal/... -coverprofile=coverage.out ./...
	local total
	total="$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%", "", $3); print $3}')"
	log_info "Cobertura total: ${total}% (mínimo ${coverage_min}%)"
	awk -v t="$total" -v m="$coverage_min" 'BEGIN { exit !(t + 0 >= m + 0) }' ||
		die "Cobertura ${total}% por debajo del ${coverage_min}%"
	success "Cobertura ${total}%"
}

lint() {
	local unformatted
	unformatted="$(gofmt -l .)"
	[[ -z "$unformatted" ]] || die "Archivos sin gofmt:\n$unformatted"
	go vet ./...
	if command -v staticcheck >/dev/null 2>&1; then
		staticcheck ./...
	else
		log_info "staticcheck no instalado — análisis adicional saltado."
	fi
	success "lint superado"
}

# E2E: el Playwright del webrelay contra este puente en modo desarrollo.
e2e() {
	local dir="${webrelay_dir:-$WORKSPACE/../ether-webrelay/source/frontend/nodejs/webrelay}"
	[[ -f "$dir/playwright.config.ts" ]] || die "No encuentro el webrelay en $dir (usa --webrelay-dir)"
	command -v ffmpeg >/dev/null 2>&1 || die "FFmpeg es necesario para arrancar el puente."
	local bin log
	port="${port:-18190}"
	bin="$(mktemp -d)/bridge"
	log="$(mktemp)"
	go build -o "$bin" ./cmd/bridge
	BRIDGE_DEV_MODE=true BRIDGE_ADDR="127.0.0.1:$port" BRIDGE_ICE_LOOPBACK=true "$bin" >"$log" 2>&1 &
	bridge_pid=$!
	trap 'kill "${bridge_pid:-}" 2>/dev/null || true' EXIT
	for _ in $(seq 1 50); do
		curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1 && break
		sleep 0.2
	done
	curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null || { cat "$log"; die "El puente no arrancó"; }
	log_info "Puente en ws://127.0.0.1:$port/signal (log: $log)"
	local channel=""
	if [[ -d "/Applications/Google Chrome.app" ]] || command -v google-chrome >/dev/null 2>&1; then channel="chrome"; fi
	(cd "$dir" && [[ -d node_modules ]] || npm ci --no-audit --no-fund)
	(cd "$dir" && E2E_BRIDGE_URL="ws://127.0.0.1:$port/signal" PW_CHANNEL="${PW_CHANNEL:-$channel}" npx playwright test) ||
		{ tail -50 "$log"; die "E2E falló"; }
	success "E2E del webrelay superado contra el puente real"
}

if [[ -n "$command_line" ]]; then
	log_info "Executing: $command_line (in $module)"
	bash -c "$command_line"
	exit 0
fi

case "$goal" in
	install)          go mod download && go mod verify ;;
	build)            mkdir -p bin && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$version" -o bin/bridge ./cmd/bridge
	                  success "bin/bridge ($version)" ;;
	test)             go test -race ./... ;;
	test-unit)        go test -race ./internal/domain/... ./internal/application/... ./tests/... ;;
	test-integration) go test -race ./internal/infrastructure/... ;;
	coverage)         coverage ;;
	lint)             lint ;;
	format)           gofmt -w . ;;
	run)              BRIDGE_DEV_MODE="${BRIDGE_DEV_MODE:-true}" BRIDGE_ADDR="${BRIDGE_ADDR:-127.0.0.1:${port:-8090}}" go run ./cmd/bridge ;;
	e2e)              e2e ;;
	*)                die "Unknown goal: $goal" ;;
esac
