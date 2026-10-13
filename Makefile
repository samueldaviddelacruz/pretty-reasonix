# Makefile for pretty-reasonix.
#
# Targets: build, cross, pure-go, test, vet, fmt, lint, clean.
#
# Only `build` and `cross` write into the tree, and both write to directories
# .gitignore covers (bin/ and dist/), so a build never leaves untracked output.

BINARY  := prx
BINDIR  := bin
DISTDIR := dist

# The release matrix. Packaging, archives and checksums are E22-T2; this target
# only produces the binaries.
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X pretty-reasonix/internal/version.Version=$(VERSION) \
	-X pretty-reasonix/internal/version.Commit=$(COMMIT) \
	-X pretty-reasonix/internal/version.Date=$(DATE)

# -trimpath keeps build-machine paths out of the binary, and CGO_ENABLED=0 is
# what makes the result a self-contained static binary
# (docs/TECHNICAL_DESIGN.md §6.1). Both matter on every target, so both are
# applied in one place.
BUILDFLAGS := -trimpath

# Overridable so a machine that keeps the linter outside PATH still works.
GOLANGCI_LINT ?= golangci-lint

.PHONY: build cross pure-go test vet fmt lint clean

# build compiles the binary for this machine into bin/.
build:
	CGO_ENABLED=0 go build $(BUILDFLAGS) -ldflags '$(LDFLAGS)' -o $(BINDIR)/$(BINARY) ./cmd/prx

# cross compiles the release matrix into dist/. set -e is inside the recipe so a
# failing target stops the loop instead of leaving a half-built matrix behind.
cross:
	@mkdir -p $(DISTDIR)
	@set -e; for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		out=$(DISTDIR)/$(BINARY)_$${os}_$${arch}; \
		printf 'building %s\n' "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(BUILDFLAGS) \
			-ldflags '$(LDFLAGS)' -o "$$out" ./cmd/prx; \
	done

# pure-go proves the whole module still builds with cgo disabled, which is what
# makes the single static binary possible (docs/DEPENDENCIES.md). `build` only
# covers ./cmd/prx, so a cgo-requiring module that nothing imports yet would
# otherwise go unnoticed until it was wired in. CI runs this from E1-T6; the
# target exists now so that ticket has one command to call.
pure-go:
	CGO_ENABLED=0 go build ./...

# test runs the suite. The test tiers are build tags (docs/TECHNICAL_DESIGN.md
# §7.1); untagged packages are the unit tier and run by default.
test:
	go test ./...
# vet runs the standard analyzers.
vet:
	go vet ./...

# fmt formats the tree in place. The check-only form is part of `make lint`,
# which runs the configured gofmt/goimports formatters.
fmt:
	gofmt -l -w .

# lint validates the configuration before running it, because a config the tool
# no longer understands would otherwise pass as "no findings".
lint:
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run

# clean removes build output.
clean:
	rm -rf $(BINDIR) $(DISTDIR)
