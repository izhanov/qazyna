package parser

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildPDF assembles a minimal but valid PDF with one page per text,
// xref offsets included, so pdftotext parses it without repairs.
func buildPDF(pageTexts []string) []byte {
	n := len(pageTexts)
	kids := make([]string, n)
	for i := range pageTexts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, text := range pageTexts {
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func writePDF(t *testing.T, pageTexts ...string) string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed (brew install poppler)")
	}
	path := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(path, buildPDF(pageTexts), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPDFParsePages(t *testing.T) {
	path := writePDF(t, "alpha bravo", "charlie delta")

	chunks, err := PDF{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(chunks), chunks)
	}

	want := []struct{ section, text string }{
		{"p. 1", "alpha bravo"},
		{"p. 2", "charlie delta"},
	}
	for i, w := range want {
		if chunks[i].Section != w.section {
			t.Errorf("chunk %d: section = %q, want %q", i, chunks[i].Section, w.section)
		}
		if chunks[i].Text != w.text {
			t.Errorf("chunk %d: text = %q, want %q", i, chunks[i].Text, w.text)
		}
		if chunks[i].Ordinal != i {
			t.Errorf("chunk %d: ordinal = %d, want %d", i, chunks[i].Ordinal, i)
		}
	}
}

func TestPDFSkipsBlankPages(t *testing.T) {
	path := writePDF(t, "", "only page with text", "")

	chunks, err := PDF{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	if chunks[0].Section != "p. 2" {
		t.Errorf("section = %q, want %q (original page number kept)", chunks[0].Section, "p. 2")
	}
}

func TestPDFScannedLikeGivesNoChunks(t *testing.T) {
	path := writePDF(t, "", "")

	chunks, err := PDF{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("got %d chunks from a textless PDF, want 0", len(chunks))
	}
}

func TestPDFInvalidFile(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	path := filepath.Join(t.TempDir(), "broken.pdf")
	if err := os.WriteFile(path, []byte("not a pdf at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (PDF{}).Parse(context.Background(), path); err == nil {
		t.Error("expected an error for a broken PDF, got nil")
	}
}
