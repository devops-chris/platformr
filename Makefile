BINARY  := platformr
VERSION ?= dev
LDFLAGS := -ldflags "-X github.com/devops-chris/platformr/cmd.Version=$(VERSION)"

.PHONY: build run test lint clean release-dry

build:
	go build $(LDFLAGS) -o bin/$(BINARY) .

run: build
	./bin/$(BINARY)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ dist/

release-dry:
	goreleaser release --snapshot --clean
