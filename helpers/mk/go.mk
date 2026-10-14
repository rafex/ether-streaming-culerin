# Targets de construcción del puente Go (regla 01).
# Delegan en helpers/shell/go.sh; nunca ejecutan go directamente.
GO_HELPER ?= helpers/shell/go.sh
GO_GOALS := install build test test-unit test-integration coverage lint format e2e

.PHONY: $(GO_GOALS)

$(GO_GOALS):
	@bash "$(GO_HELPER)" --goal "$@" --module "$(MODULE)" --version "$(VERSION)"
