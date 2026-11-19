# Development helpers for the ConduitOmniChat workspace.
GO ?= go
BIN := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO_PKG := github.com/cestila/ConduitOmniChat/core/pkg/buildinfo
LDFLAGS ?= -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).Date=$(BUILD_DATE)

.PHONY: proto build daemon test test-go test-gui clean

proto: ## Regenerate sdk/conduitv1 from sdk/proto/conduit.proto
	protoc -I sdk/proto \
	  --go_out=sdk/conduitv1 --go_opt=paths=source_relative \
	  --go-grpc_out=sdk/conduitv1 --go-grpc_opt=paths=source_relative \
	  sdk/proto/conduit.proto

build: daemon

daemon:
	cd core && $(GO) build -ldflags "$(LDFLAGS)" -o ../$(BIN)/conduitd ./cmd/conduitd

test: test-go test-gui

test-go:
	@for m in sdk core; do \
	  (cd $$m && $(GO) test ./...) || exit 1; \
	done

test-gui:
	cd gui && npm test

clean:
	rm -rf $(BIN)
