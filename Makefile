VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
PREFIX  ?= /usr
GO      ?= go

.PHONY: all build test check install clean

all: build

build:
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/mcp-docs ./cmd/mcp-docs

test:
	$(GO) test ./...

check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: the files above need formatting"; exit 1; }
	$(GO) vet ./...
	$(GO) test -race ./...

install: build
	install -D -m 0755 bin/mcp-docs $(DESTDIR)$(PREFIX)/bin/mcp-docs

clean:
	rm -rf bin
