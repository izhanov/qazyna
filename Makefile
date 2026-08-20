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

# Platform + platform-specific linker flags
ifeq ($(UNAME_S),Darwin)
	PLATFORM := darwin
	PLATFORM_LIBS := -framework Security -framework CoreFoundation
	BIN := qazyna
else ifeq ($(UNAME_S),Linux)
	PLATFORM := linux
	PLATFORM_LIBS := -lm -ldl -lpthread
	BIN := qazyna
else ifneq (,$(findstring MINGW,$(UNAME_S)))
	PLATFORM := windows
	PLATFORM_LIBS :=
	BIN := qazyna.exe
else
	$(error Unsupported platform: $(UNAME_S))
endif

LIB_DIR     := $(CURDIR)/lib/$(PLATFORM)_$(ARCH)
CGO_CFLAGS  := -I$(CURDIR)/include
CGO_LDFLAGS := $(LIB_DIR)/liblancedb_go.a $(PLATFORM_LIBS)

.PHONY: build run clean platform-info

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" go build -o bin/$(BIN) ./cmd/qazyna

run: build
	./bin/$(BIN) $(ARGS)

platform-info:
	@echo "platform: $(PLATFORM)_$(ARCH)"
	@echo "lib dir:  $(LIB_DIR)"
	@echo "ldflags:  $(CGO_LDFLAGS)"

clean:
	rm -rf bin data
