package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseVersionRe(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.2.0":                true,
		"v10.20.30":             true,
		"dev":                   false,
		"v0.1.0-9-gabc123":      false,
		"v0.1.0-dirty":          false,
		"v0.1.0-9-gabc12-dirty": false,
	} {
		if got := releaseVersionRe.MatchString(v); got != want {
			t.Errorf("releaseVersionRe(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestExtractBinary(t *testing.T) {
	archive := func(name, content string) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return &buf
	}

	dest := filepath.Join(t.TempDir(), "qazyna")
	if err := extractBinary(archive("qazyna", "#!binary"), dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "#!binary" {
		t.Fatalf("extracted %q, %v", data, err)
	}
	if info, _ := os.Stat(dest); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}

	if err := extractBinary(archive("README.md", "docs"), filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("archive without the binary: expected error")
	}
}
