BUMP_VERSION := $(GOPATH)/bin/bump_version
WRITE_MAILMAP := $(GOPATH)/bin/write_mailmap

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run github.com/kevinburke/differ@latest gofmt -s -w .

test:
	@# the timeout helps guard against infinite recursion
	go test -timeout=250ms ./...

race-test:
	go test -timeout=500ms -race ./...

coverage:
	go test -trimpath -timeout=250ms -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

$(BUMP_VERSION):
	go get -u github.com/kevinburke/bump_version

$(WRITE_MAILMAP):
	go get -u github.com/kevinburke/write_mailmap

release: test | $(BUMP_VERSION)
	$(BUMP_VERSION) --tag-prefix=v minor config.go

force: ;

AUTHORS.txt: force | $(WRITE_MAILMAP)
	$(WRITE_MAILMAP) > AUTHORS.txt

authors: AUTHORS.txt
