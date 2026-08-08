MODULE  := github.com/javiervargas02/awake
BINARY  := awake
PKG     := ./cmd/awake

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Stamp identity into the binary rather than reading it from disk at runtime
# (principle 4).
LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.date=$(DATE)

.PHONY: build test test-system check fmt vet race clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test ./...

race:
	go test -race ./...

# System tests drive real macOS power assertions, including the orphan
# guarantee (ADR-0006). They are slow, need a real machine, and must run before
# every release: an orphaned assertion is the worst bug this project can ship.
test-system:
	go test -tags system -count=1 -v ./internal/platform/...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

# What CI runs on every commit.
check: vet race
	@gofmt -l . | grep . && { echo "unformatted files above; run 'make fmt'"; exit 1; } || echo "ok"

clean:
	rm -f $(BINARY)
