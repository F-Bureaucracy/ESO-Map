VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X github.com/vincent/eso-map/internal/cli.version=$(VERSION) -X github.com/vincent/eso-map/internal/cli.commit=$(COMMIT)

.PHONY: build test lint example clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/eso-map .

test:
	go test -race ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

example: build
	./bin/eso-map scan examples -o html --out bin/example.html

clean:
	rm -rf bin
