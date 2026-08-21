package parser

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

// DOCX parses Microsoft Word documents (.docx format).
// DOCX files are ZIP archives containing XML. This parser extracts text from word/document.xml
// and preserves heading hierarchy for better chunking.
//
// Note: .doc (binary) format is not supported. For .doc support, see github.com/unidoc/unioffice.
type DOCX struct{}

func (DOCX) Extensions() []string { return []string{".docx"} }

// Parse extracts text chunks from a DOCX file (ZIP archive with XML content).
func (DOCX) Parse(_ context.Context, path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, err
	}

	// Extract text from word/document.xml
	var documentXML io.ReadCloser
	for _, zf := range zr.File {
		if zf.Name == "word/document.xml" {
			documentXML, err = zf.Open()
			if err != nil {
				return nil, err
			}
			defer documentXML.Close()
			break
		}
	}
	if documentXML == nil {
		return nil, nil // Empty or malformed docx
	}

	// Parse XML and extract text
	doc := &wordDocument{}
	if err := xml.NewDecoder(documentXML).Decode(doc); err != nil {
		return nil, err
	}

	// Convert to chunks
	return doc.toChunks(), nil
}

// Minimal Word XML structure
// Uses xml namespace "w" for Word elements
type wordDocument struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main document"`
	Body    wordBody `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main body"`
}

type wordBody struct {
	Paragraphs []wordParagraph `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main p"`
}

type wordParagraph struct {
	Properties *wordParagraphProperties `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
	Runs       []wordRun                `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main r"`
}

type wordParagraphProperties struct {
	Style *wordStyle `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pStyle"`
}

type wordStyle struct {
	Val string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
}

type wordRun struct {
	Text []string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main t"`
}

func (d *wordDocument) toChunks() []Chunk {
	var chunks []Chunk
	var headings [7]string // heading levels 1..6
	var body []string
	ordinal := 0

	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if text == "" {
			return
		}
		section := sectionName(headings)
		for _, part := range splitByParagraphs(text) {
			chunks = append(chunks, Chunk{Section: section, Text: part, Ordinal: ordinal})
			ordinal++
		}
	}

	for _, para := range d.Body.Paragraphs {
		// Extract text from paragraph
		text := ""
		for _, run := range para.Runs {
			for _, t := range run.Text {
				text += t
			}
		}

		// Check if this is a heading
		if para.Properties != nil && para.Properties.Style != nil {
			style := para.Properties.Style.Val
			headingLevel := parseHeadingLevel(style)
			if headingLevel > 0 {
				flush()
				headings[headingLevel] = text
				// Clear sub-levels
				for i := headingLevel + 1; i < len(headings); i++ {
					headings[i] = ""
				}
				continue
			}
		}

		// Regular paragraph
		if text != "" {
			body = append(body, text)
		}
	}
	flush()

	return chunks
}

// parseHeadingLevel extracts heading level from Word style name
// "Heading1" -> 1, "Heading2" -> 2, etc.
func parseHeadingLevel(style string) int {
	if !strings.HasPrefix(style, "Heading") {
		return 0
	}
	rest := strings.TrimPrefix(style, "Heading")
	if len(rest) == 0 {
		return 0
	}
	// Simple: "Heading1" -> '1' -> 1
	if rest[0] >= '1' && rest[0] <= '6' {
		return int(rest[0] - '0')
	}
	return 0
}
