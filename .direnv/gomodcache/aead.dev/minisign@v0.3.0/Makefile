ifneq ($(shell go env GOBIN),)
	GOBIN := $(shell go env GOBIN)
else
	GOBIN := $(shell $(go env GOPATH)/bin)
endif

.PHONY: build check release test lint update-tools

build:
	@mkdir -m 0755 -p ${GOBIN}
	@CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ${GOBIN}/minisign ./cmd/minisign

check:
	@gofmt -d . && echo No formatting issue found.
	@govulncheck ./...
		
release:
ifneq ($(shell git status -s) , )
	@(echo "Repository contains modified files." && exit 1)
else
	@echo -n Building minisign ${VERSION} for linux/amd64...
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./minisign ./cmd/minisign
	@tar -czf minisign-linux-amd64.tar.gz ./minisign ./LICENSE ./README.md
	@echo " DONE."

	@echo -n Building minisign ${VERSION} for linux/arm64...
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./minisign ./cmd/minisign
	@tar -czf minisign-linux-arm64.tar.gz ./minisign ./LICENSE ./README.md
	@echo " DONE."

	@echo -n Building minisign ${VERSION} for darwin/arm64...
	@GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./minisign ./cmd/minisign
	@tar -czf minisign-darwin-arm64.tar.gz ./minisign ./LICENSE ./README.md
	@echo " DONE."

	@echo -n Building minisign ${VERSION} for windows/amd64...
	@GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./minisign ./cmd/minisign
	@zip -q minisign-windows-amd64.zip ./minisign ./LICENSE ./README.md
	@echo " DONE."

	@rm ./minisign
endif

test:
	@CGO_ENABLED=0 go test -ldflags "-s -w" ./...

lint:
	@go vet ./...
	@golangci-lint run --config ./.golangci.yml

update-tools:
	@CGO_ENABLED=0 go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@CGO_ENABLED=0 go install golang.org/x/vuln/cmd/govulncheck@latest
