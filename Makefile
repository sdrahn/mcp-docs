VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
PREFIX     ?= /usr
BINDIR     ?= $(PREFIX)/bin
DATADIR    ?= $(PREFIX)/share
SYSCONFDIR ?= /etc
GO         ?= go
# Position-independent executables, as openSUSE's rpmlint asks for.
BUILDMODE  ?= pie

.PHONY: all build test check eval install clean

all: build

build:
	$(GO) build -trimpath -buildmode=$(BUILDMODE) -ldflags "-X main.version=$(VERSION)" -o bin/mcp-docs ./cmd/mcp-docs

test:
	$(GO) test ./...

check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: the files above need formatting"; exit 1; }
	$(GO) vet ./...
	$(GO) test -race ./...

# What answers cost on a fixed corpus (internal/eval): a table of bytes
# per question against reading the whole document.
eval:
	$(GO) test -count=1 -run TestCost -v ./internal/eval

# The program, and the directories of collection files: packages ship
# theirs below $(DATADIR)/mcp-docs/collections.d, the administrator's go
# to $(SYSCONFDIR)/mcp-docs/collections.d.
# It installs what build built (with its VERSION), building only if
# nothing was.
install:
	test -x bin/mcp-docs || $(MAKE) build
	install -D -m 0755 bin/mcp-docs $(DESTDIR)$(BINDIR)/mcp-docs
	install -d -m 0755 $(DESTDIR)$(DATADIR)/mcp-docs/collections.d
	install -d -m 0755 $(DESTDIR)$(SYSCONFDIR)/mcp-docs/collections.d

clean:
	rm -rf bin
