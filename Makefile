VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOEXE := $(shell go env GOEXE)
PREFIX ?= $(HOME)/.local
INSTALLDIR := $(PREFIX)/bin

.PHONY: build vet fmt test hooks cross clean install

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/reasonix$(GOEXE) ./cmd/reasonix
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/reasonix-plugin-example$(GOEXE) ./cmd/reasonix-plugin-example

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

hooks:
	@git config core.hooksPath .githooks
	@echo "installed: core.hooksPath -> .githooks (pre-push runs go vet)"

cross:
	@mkdir -p dist
	@for p in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/reasonix-$$os-$$arch$$ext ./cmd/reasonix; \
	done

clean:
	rm -rf bin dist

install: build
	@if [ -f "$(INSTALLDIR)/reasonix$(GOEXE)" ]; then \
		if [ "$(FORCE)" != "1" ]; then \
			echo "error: $(INSTALLDIR)/reasonix$(GOEXE) already exists."; \
			echo "To overwrite, run: make install FORCE=1"; \
			exit 1; \
		fi; \
		echo "warning: overwriting existing installation (FORCE=1)"; \
	fi
	@mkdir -p "$(INSTALLDIR)"
	cp bin/reasonix$(GOEXE) "$(INSTALLDIR)/reasonix$(GOEXE)"
	chmod 0755 "$(INSTALLDIR)/reasonix$(GOEXE)"
	cp bin/reasonix-plugin-example$(GOEXE) "$(INSTALLDIR)/reasonix-plugin-example$(GOEXE)"
	chmod 0755 "$(INSTALLDIR)/reasonix-plugin-example$(GOEXE)"
	@echo "installed: $(INSTALLDIR)/reasonix$(GOEXE)"
	@echo "installed: $(INSTALLDIR)/reasonix-plugin-example$(GOEXE)"
	@echo ""
	@echo "Make sure $(INSTALLDIR) is in your PATH."
	@echo "Then run: reasonix setup"
