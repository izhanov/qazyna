UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Normalize architecture: x86_64 -> amd64, aarch64 -> arm64
ifeq ($(UNAME_M),x86_64)
	ARCH := amd64
else ifeq ($(UNAME_M),arm64)
	ARCH := arm64
else ifeq ($(UNAME_M),aarch64)
	ARCH := arm64
else
	$(error Unsupported architecture: $(UNAME_M))
endif

# Platform detection: used to locate the native LanceDB library.
# The CGO flags themselves live in internal/store/cgo_flags.go, so plain
# `go build` / `go test ./...` work without any environment variables.
ifeq ($(UNAME_S),Darwin)
	PLATFORM := darwin
	BIN := qazyna
else ifeq ($(UNAME_S),Linux)
	PLATFORM := linux
	BIN := qazyna
else ifneq (,$(findstring MINGW,$(UNAME_S)))
	PLATFORM := windows
	BIN := qazyna.exe
else
	$(error Unsupported platform: $(UNAME_S))
endif

LIB_DIR := $(CURDIR)/lib/$(PLATFORM)_$(ARCH)

LANCEDB_VERSION := v0.1.2
LANCEDB_LIB     := $(LIB_DIR)/liblancedb_go.a

VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_FLAGS := -ldflags "-X qazyna/internal/cli.version=$(VERSION)"

.PHONY: build run test install clean deps clean-deps platform-info

# Native LanceDB libraries: downloaded once, skipped if already present.
# The per-platform release assets of v0.1.2 overwrote each other at upload
# (all were named liblancedb_go.a), so only the combined tarball contains
# correct libraries for every platform. Extract just ours plus the header.
$(LANCEDB_LIB):
	curl -sSL https://github.com/lancedb/lancedb-go/releases/download/$(LANCEDB_VERSION)/lancedb-go-native-binaries.tar.gz \
		| tar -xzf - lib/$(PLATFORM)_$(ARCH) include
	ranlib $(LANCEDB_LIB) 2>/dev/null || true

deps: $(LANCEDB_LIB)

build: $(LANCEDB_LIB)
	go build $(GO_FLAGS) -o bin/$(BIN) ./cmd/qazyna

run: build
	./bin/$(BIN) $(ARGS)

test: $(LANCEDB_LIB)
	go test -race ./...

install: $(LANCEDB_LIB)
	go install $(GO_FLAGS) ./cmd/qazyna

platform-info:
	@echo "platform: $(PLATFORM)_$(ARCH)"
	@echo "lib dir:  $(LIB_DIR)"

clean:
	rm -rf bin data

clean-deps:
	rm -rf lib include
