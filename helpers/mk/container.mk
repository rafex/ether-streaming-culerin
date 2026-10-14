# Targets de contenedor (reglas 06 y 08): podman → docker, Containerfile.
.PHONY: runtime image ci app-image

CONTAINER_HELPER ?= helpers/shell/container.sh
CONTAINER_RUNTIME ?=
CI_IMAGE ?= ether-bridge-ci:local
CI_CONTAINERFILE ?= containers/ci/Containerfile
APP_IMAGE ?= ether-bridge:local
APP_CONTAINERFILE ?= containers/bridge/Containerfile

CONTAINER_FLAGS = --project-name "$(PROJECT_NAME)" --container-runtime "$(CONTAINER_RUNTIME)" \
	--ci-image "$(CI_IMAGE)" --ci-containerfile "$(CI_CONTAINERFILE)" \
	--app-image "$(APP_IMAGE)" --app-containerfile "$(APP_CONTAINERFILE)" \
	--module "$(MODULE)" --version "$(VERSION)"

runtime image ci app-image:
	@bash "$(CONTAINER_HELPER)" --action "$@" $(CONTAINER_FLAGS)
