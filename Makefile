# Makefile for pretty-reasonix.
#
# This ticket (E1-T1) only needs `make build`. E1-T2 adds the rest of the
# matrix: cross-compilation, test, vet, fmt and lint targets, so that this file
# stays small until there is a reason for it to grow.

BINARY  := bin/prx
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X pretty-reasonix/internal/version.Version=$(VERSION) \
	-X pretty-reasonix/internal/version.Commit=$(COMMIT) \
	-X pretty-reasonix/internal/version.Date=$(DATE)

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/prx
