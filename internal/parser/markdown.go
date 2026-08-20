package parser

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strings"
)

// maxChunkChars keeps chunks comfortably within the embedding model context.
const maxChunkChars = 1500

type Markdown struct{}

func (Markdown) Extensions() []string { return []string{".md", ".markdown"} }

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

func (Markdown) Parse(_ context.Context, path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		chunks   []Chunk
		headings [7]string // index = heading level, 1..6
		body     []string
		inFence  bool
		ordinal  int
	)

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

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			body = append(body, line)
			continue
		}
		if !inFence {
			if m := headingRe.FindStringSubmatch(line); m != nil {
				flush()
				level := len(m[1])
				headings[level] = strings.TrimSpace(m[2])
				for i := level + 1; i < len(headings); i++ {
					headings[i] = ""
				}
				continue
			}
		}
		body = append(body, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()

	return chunks, nil
}

func sectionName(headings [7]string) string {
	var parts []string
	for _, h := range headings {
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, " > ")
}

var blankLineRe = regexp.MustCompile(`\n\s*\n`)

// splitByParagraphs packs paragraphs into chunks of at most maxChunkChars;
// a single oversized paragraph is hard-split.
func splitByParagraphs(text string) []string {
	var (
		parts []string
		cur   strings.Builder
	)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			parts = append(parts, s)
		}
		cur.Reset()
	}

	for _, p := range blankLineRe.Split(text, -1) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > maxChunkChars {
			flush()
			parts = append(parts, hardSplit(p, maxChunkChars)...)
			continue
		}
		if cur.Len() > 0 && cur.Len()+2+len(p) > maxChunkChars {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	flush()

	return parts
}

func hardSplit(s string, maxChars int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := min(maxChars, len(runes))
		out = append(out, strings.TrimSpace(string(runes[:n])))
		runes = runes[n:]
	}
	return out
}
