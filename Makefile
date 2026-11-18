# Development helpers for the ConduitOmniChat workspace.
GO ?= go
BIN := bin
PLUGINS := whatsapp telegram discord
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO_PKG := github.com/cestila/ConduitOmniChat/core/pkg/buildinfo
LDFLAGS ?= -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).Date=$(BUILD_DATE)

.PHONY: proto build daemon plugins tui test test-go test-gui clean

proto: ## Regenerate sdk/conduitv1 from sdk/proto/conduit.proto
	protoc -I sdk/proto \
	  --go_out=sdk/conduitv1 --go_opt=paths=source_relative \
	  --go-grpc_out=sdk/conduitv1 --go-grpc_opt=paths=source_relative \
	  sdk/proto/conduit.proto

build: daemon plugins

daemon:
	cd core && $(GO) build -ldflags "$(LDFLAGS)" -o ../$(BIN)/conduitd ./cmd/conduitd

plugins:
	@for p in $(PLUGINS); do \
	  if [ -d plugins/$$p ]; then (cd plugins/$$p && $(GO) build -o ../../$(BIN)/plugins/conduit-plugin-$$p .) || exit 1; fi; \
	done

tui:
	@if [ -d tui ]; then cd tui && $(GO) build -o ../$(BIN)/conduit-tui .; else echo "tui is not part of this repository"; fi

test: test-go test-gui

test-go:
	@for m in sdk core $(addprefix plugins/,$(PLUGINS)); do \
	  if [ -d $$m ]; then (cd $$m && $(GO) test ./...) || exit 1; fi; \
	done

test-gui:
	cd gui && npm test

clean:
	rm -rf $(BIN)
