package parser

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDOCX_Extensions(t *testing.T) {
	d := DOCX{}
	exts := d.Extensions()
	if len(exts) == 0 || exts[0] != ".docx" {
		t.Fatalf("Expected [.docx], got %v", exts)
	}
}

func TestDOCX_Parse(t *testing.T) {
	// Create a minimal valid DOCX file (it's just a ZIP)
	tmpdir := t.TempDir()
	docxPath := filepath.Join(tmpdir, "test.docx")

	// Create a minimal DOCX structure
	createTestDOCX(t, docxPath)

	d := DOCX{}
	chunks, err := d.Parse(context.Background(), docxPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	// Check that we got some content
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Text, "Heading 1") || strings.Contains(chunk.Text, "Body text") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Expected to find 'Heading 1' or 'Body text' in chunks, got: %+v", chunks)
	}
}

func createTestDOCX(t *testing.T, path string) {
	// Minimal DOCX structure: just a ZIP with word/document.xml
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create test docx: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	// Create word/document.xml
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr>
        <w:pStyle w:val="Heading1"/>
      </w:pPr>
      <w:r>
        <w:t>Heading 1</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:r>
        <w:t>Body text paragraph</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:pPr>
        <w:pStyle w:val="Heading2"/>
      </w:pPr>
      <w:r>
        <w:t>Heading 2</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:r>
        <w:t>More body text</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`

	fw, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("Failed to create document.xml in ZIP: %v", err)
	}
	if _, err := fw.Write([]byte(docXML)); err != nil {
		t.Fatalf("Failed to write document.xml: %v", err)
	}
}
