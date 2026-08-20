CGO_CFLAGS  := -I$(CURDIR)/include
CGO_LDFLAGS := $(CURDIR)/lib/darwin_arm64/liblancedb_go.a -framework Security -framework CoreFoundation

.PHONY: build run clean

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" go build -o bin/qazyna ./cmd/qazyna

run: build
	./bin/qazyna $(ARGS)

clean:
	rm -rf bin data
