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

.PHONY: build run test install clean deps clean-deps platform-info

# Native LanceDB libraries: downloaded once, skipped if already present
$(LANCEDB_LIB):
	curl -sSL https://raw.githubusercontent.com/lancedb/lancedb-go/main/scripts/download-artifacts.sh | bash -s $(LANCEDB_VERSION)

deps: $(LANCEDB_LIB)

build: $(LANCEDB_LIB)
	go build -o bin/$(BIN) ./cmd/qazyna

run: build
	./bin/$(BIN) $(ARGS)

test: $(LANCEDB_LIB)
	go test ./...

install: $(LANCEDB_LIB)
	go install ./cmd/qazyna

platform-info:
	@echo "platform: $(PLATFORM)_$(ARCH)"
	@echo "lib dir:  $(LIB_DIR)"

clean:
	rm -rf bin data

clean-deps:
	rm -rf lib include
