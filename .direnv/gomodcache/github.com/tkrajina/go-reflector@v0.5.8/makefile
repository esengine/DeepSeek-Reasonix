.PHONY: test
test: lint
	go test -race ./...

.PHONY: test-performance
test-performance:
	N=1000000 go test -race -v ./... -run=TestPerformance

.PHONY: install
install: check test
	go install ./...

.PHONY: lint
lint:
	go vet ./...
	golangci-lint run

.PHONY: install-tools
install-tools:
	go get -u github.com/fzipp/gocyclo
	go get -u github.com/golang/lint
	go get -u github.com/kisielk/errcheck
