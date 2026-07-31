TS ?= tree-sitter

all install uninstall clean:
	$(MAKE) -C php $@
	$(MAKE) -C php_only $@

generate:
	cd php && $(TS) generate
	cd php_only && $(TS) generate

test:
	$(TS) test

.PHONY: all install uninstall clean test update generate
