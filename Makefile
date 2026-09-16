# bnt — build-and-test for CMake
#
# Common targets:
#   make              build ./bnt
#   make install      install to $(PREFIX)/bin (default /usr/local)
#   make install PREFIX=~/.local
#   make test         go test ./...
#   make clean        remove the built binary

BINARY  := bnt
PREFIX  ?= /usr/local
DESTDIR ?=

# make does not perform tilde expansion, and some shells (zsh) don't expand ~
# in a `PREFIX=~/x` argument either — so expand a leading ~ / ~/ ourselves.
# `override` is required because PREFIX may be set on the command line, and a
# command-line assignment otherwise wins over a plain makefile assignment.
# Recipe paths are then quoted safely (no tilde left to suppress).
override PREFIX := $(patsubst ~,$(HOME),$(patsubst ~/%,$(HOME)/%,$(PREFIX)))
BINDIR  := $(DESTDIR)$(PREFIX)/bin

GO      ?= go
GOFLAGS ?=

.PHONY: all build install uninstall test vet clean

all: build

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) .

install: build
	mkdir -p "$(BINDIR)"
	install -m 0755 $(BINARY) "$(BINDIR)/$(BINARY)"
	@echo "installed $(BINARY) to $(BINDIR)/$(BINARY)"

uninstall:
	rm -f "$(BINDIR)/$(BINARY)"
	@echo "removed $(BINDIR)/$(BINARY)"

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)
