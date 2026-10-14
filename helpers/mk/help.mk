TARGET ?=
MAN_HELPER ?= helpers/shell/man.sh

.PHONY: man

man:
	@if [ -z "$(TARGET)" ]; then \
		echo "Uso: make man TARGET=<tarea>  (ej: make man TARGET=build)"; \
		exit 1; \
	fi
	@bash "$(MAN_HELPER)" --target "$(TARGET)"
