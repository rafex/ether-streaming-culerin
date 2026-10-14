.PHONY: clean

clean:
	@bash helpers/shell/clean.sh --paths "$(SITE_DIR) $(MODULE)/bin $(MODULE)/coverage.out"
