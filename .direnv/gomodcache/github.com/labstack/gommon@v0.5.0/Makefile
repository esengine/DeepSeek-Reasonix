PKG := "github.com/labstack/gommon"
PKG_LIST := $(shell go list ${PKG}/...)

.DEFAULT_GOAL := check
check: lint vet race ## Check project

init:
	@go install golang.org/x/lint/golint@latest

lint: ## Lint the files
	@golint -set_exit_status ${PKG_LIST}

vet: ## Vet the files
	@go vet ${PKG_LIST}

test: ## Run tests
	@go test -short ${PKG_LIST}

race: ## Run tests with data race detector
	@go test -race ${PKG_LIST}

benchmark: ## Run benchmarks
	@go test -run="-" -bench=".*" ${PKG_LIST}

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

goversion ?= "1.20"
test_version: ## Run tests inside Docker with given version (defaults to 1.20). Example: make test_version goversion=1.20
	@docker run --rm -it -v $(shell pwd):/project golang:$(goversion) /bin/sh -c "cd /project && make init check"
