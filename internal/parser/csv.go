package parser

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// CSV indexes tabular files row by row. Column headers are woven into the
// text ("name: Айгерим"), so every value keeps its meaning for the embedding;
// small rows are packed together up to the chunk size limit.
type CSV struct{}

func (CSV) Extensions() []string { return []string{".csv", ".tsv"} }

func (CSV) Parse(_ context.Context, path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	r := csv.NewReader(br)
	r.Comma = sniffDelimiter(br, filepath.Ext(path))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var (
		chunks    []Chunk
		ordinal   int
		buf       []string // rendered rows of the current chunk
		bufRunes  int
		firstLine int // 1-based file line of the first row in buf
		line      = 1 // header is line 1
	)
	flush := func(lastLine int) {
		if len(buf) == 0 {
			return
		}
		section := fmt.Sprintf("row %d", firstLine)
		if lastLine > firstLine {
			section = fmt.Sprintf("rows %d-%d", firstLine, lastLine)
		}
		for _, part := range splitByParagraphs(strings.Join(buf, "\n\n")) {
			chunks = append(chunks, Chunk{Section: section, Text: part, Ordinal: ordinal})
			ordinal++
		}
		buf, bufRunes = nil, 0
	}

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line++

		row := renderCSVRow(header, record)
		if row == "" {
			continue
		}
		n := utf8.RuneCountInString(row)
		if bufRunes > 0 && bufRunes+n+2 > maxChunkChars {
			flush(line - 1)
		}
		if len(buf) == 0 {
			firstLine = line
		}
		buf = append(buf, row)
		bufRunes += n + 2
	}
	flush(line)

	return chunks, nil
}

// renderCSVRow turns a record into self-describing "header: value" lines.
func renderCSVRow(header, record []string) string {
	var lines []string
	for i, v := range record {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		name := fmt.Sprintf("col %d", i+1)
		if i < len(header) && strings.TrimSpace(header[i]) != "" {
			name = strings.TrimSpace(header[i])
		}
		lines = append(lines, name+": "+v)
	}
	return strings.Join(lines, "\n")
}

// sniffDelimiter peeks at the first line and picks the most frequent of
// comma, semicolon and tab; the extension gives .tsv a head start.
func sniffDelimiter(br *bufio.Reader, ext string) rune {
	best := ','
	if strings.EqualFold(ext, ".tsv") {
		best = '\t'
	}
	peek, _ := br.Peek(4096)
	line, _, _ := strings.Cut(string(peek), "\n")
	max := 0
	for _, d := range []rune{',', ';', '\t'} {
		if n := strings.Count(line, string(d)); n > max {
			best, max = d, n
		}
	}
	return best
}
