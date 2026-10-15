.PHONY: help ci-local

# Punto de entrada universal — solo construcción/artefactos (regla 01).
# La lógica vive en helpers/shell; los targets en helpers/mk/*.mk.
# Operativas del día a día → Justfile (just --list).

PROJECT_NAME ?= ether-streaming-culerin
LANG ?= go
MODULE ?= source/backend/go/bridge
VERSION ?= dev
SITE_DIR ?= site
MK_FILES ?= $(wildcard helpers/mk/*.mk)

export WORKSPACE := $(CURDIR)
export PROJECT_NAME

-include $(MK_FILES)

.DEFAULT_GOAL := help

# Gate completo sin contenedor (lo que ejecuta la imagen de CI).
ci-local: install lint build coverage

help:
	@echo "Makefile — Construcción (build/artefactos) de $(PROJECT_NAME)"
	@echo "  make install           - Descargar y verificar módulos (go mod)"
	@echo "  make build             - Binario estático en $(MODULE)/bin/bridge"
	@echo "  make test              - Todos los tests (con -race)"
	@echo "  make test-unit         - Dominio, aplicación y arquitectura"
	@echo "  make test-integration  - Adaptadores (Pion, FFmpeg, HTTP)"
	@echo "  make coverage          - Tests con cobertura (umbral 80%)"
	@echo "  make lint | format     - gofmt + go vet (+ staticcheck si existe)"
	@echo "  make vuln              - govulncheck: vulnerabilidades conocidas que el código alcanza"
	@echo "  make e2e               - E2E del webrelay contra este puente"
	@echo "  make clean             - Limpiar artefactos"
	@echo "  make docs              - Sitio MkDocs en $(SITE_DIR)/"
	@echo ""
	@echo "  make runtime           - Detectar podman | docker"
	@echo "  make image             - Imagen de CI (containers/ci/Containerfile)"
	@echo "  make ci                - install + lint + build + coverage en contenedor"
	@echo "  make app-image         - Imagen del puente (containers/bridge/Containerfile)"
	@echo ""
	@echo "  make pre-commit | pre-push   - Gates de git hooks"
	@echo "  make man TARGET=<tarea>      - Manual (docs/man_<tarea>.md)"
	@echo ""
	@echo "Variables: MODULE=$(MODULE) VERSION=$(VERSION)"
