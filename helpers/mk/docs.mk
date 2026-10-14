.PHONY: docs
DOCS_FILE ?= .config/mkdocs/mkdocs.yml

docs:
	@bash helpers/shell/docs.sh --goal build --file "$(DOCS_FILE)" --site-dir "$(SITE_DIR)"
