package store

// Линковка с нативной библиотекой LanceDB (скачивается через `make deps`).
// Флаги здесь дублируют логику Makefile, но позволяют обычным
// `go build` / `go test ./...` работать без CGO-переменных окружения.

/*
#cgo CFLAGS: -I${SRCDIR}/../../include
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../lib/darwin_arm64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../lib/darwin_amd64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/../../lib/linux_arm64/liblancedb_go.a -lm -ldl -lpthread
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/../../lib/linux_amd64/liblancedb_go.a -lm -ldl -lpthread
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/../../lib/windows_amd64/liblancedb_go.a
*/
import "C"
