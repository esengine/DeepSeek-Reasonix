VERSION := $(shell \
	tag=$$(git tag --merged HEAD --list 'desktop-v[0-9]*' --sort=-v:refname 2>/dev/null | head -n 1); \
	if [ -n "$$tag" ]; then \
		git describe --tags --dirty --match "$$tag" 2>/dev/null; \
	else \
		git describe --tags --always --dirty 2>/dev/null || echo dev; \
	fi)
BUILD_NUMBER := $(shell date -u +%Y%m%d%H%M%S)
BUILD_TIME_UTC := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY := $(shell test -z "$$(git status --porcelain --untracked-files=no 2>/dev/null)" && echo clean || echo dirty)
BUILD_PROFILE ?= release
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.buildNumber=$(BUILD_NUMBER) \
	-X main.buildTimeUTC=$(BUILD_TIME_UTC) \
	-X main.gitCommit=$(GIT_COMMIT) \
	-X main.gitDirty=$(GIT_DIRTY) \
	-X main.buildProfile=$(BUILD_PROFILE)
GOEXE := $(shell go env GOEXE)

.PHONY: build vet fmt test desktop-test desktop-test-short desktop-test-times hooks cross clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/reasonix$(GOEXE) ./cmd/reasonix
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/reasonix-plugin-example$(GOEXE) ./cmd/reasonix-plugin-example

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

desktop-test:
	cd desktop && go test .

desktop-test-short:
	cd desktop && go test -short .

desktop-test-times:
	cd desktop && go test -count=1 -json . | python3 ../scripts/desktop-test-times.py

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
