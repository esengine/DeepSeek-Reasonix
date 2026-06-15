VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOEXE := $(shell go env GOEXE)

# CodeGraph release pinned for the bundled MCP server / e2e test. Bump together
# with any change to the integration in internal/codegraph.
CODEGRAPH_VERSION := v0.9.7

.PHONY: build vet fmt test hooks cross clean e2e-codegraph

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

# Fetch the matching CodeGraph bundle into bin/codegraph/ (the distribution
# layout: launcher at bin/codegraph/bin/codegraph beside bin/reasonix) and run the
# gated MCP end-to-end test against it. Requires `gh`. Windows: install via the
# upstream install.ps1 and run the test with REASONIX_CODEGRAPH_BIN set.
e2e-codegraph:
	@os=$$(uname -s | tr 'A-Z' 'a-z'); arch=$$(uname -m); \
	case $$arch in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=x64;; *) echo "unsupported arch $$arch"; exit 1;; esac; \
	asset=codegraph-$$os-$$arch.tar.gz; dest=bin/codegraph; \
	echo "fetching $$asset ($(CODEGRAPH_VERSION)) -> $$dest"; \
	rm -rf $$dest && mkdir -p $$dest; \
	gh release download $(CODEGRAPH_VERSION) -R colbymchenry/codegraph -p $$asset -O /tmp/$$asset; \
	tar -xzf /tmp/$$asset -C $$dest --strip-components=1; \
	REASONIX_CODEGRAPH_E2E=1 REASONIX_CODEGRAPH_BIN=$$PWD/$$dest/bin/codegraph \
		go test ./internal/codegraph/ -run E2E -v -count=1
