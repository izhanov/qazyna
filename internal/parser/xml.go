package parser

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// XML indexes the text content of XML documents. Markup itself is noise for
// embeddings, so tags never reach the chunk text — but tag names are context,
// not garbage: the element path becomes the Section ("catalog > book"), leaf
// values are rendered as "name: value" lines, attributes likewise.
type XML struct{}

func (XML) Extensions() []string { return []string{".xml"} }

func (XML) Parse(_ context.Context, path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	root, err := parseXMLTree(f)
	if err != nil {
		return nil, err
	}

	var (
		chunks  []Chunk
		ordinal int
	)
	for _, el := range root.children {
		chunkXMLNode(el, el.name, &chunks, &ordinal)
	}
	return chunks, nil
}

type xmlNode struct {
	name     string
	attrs    []xml.Attr
	text     strings.Builder
	children []*xmlNode
}

func parseXMLTree(r io.Reader) (*xmlNode, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	root := &xmlNode{}
	stack := []*xmlNode{root}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{name: t.Name.Local, attrs: t.Attr}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			stack[len(stack)-1].text.Write(t)
		}
	}
	return root, nil
}

// chunkXMLNode walks top-down: the largest subtree that fits maxChunkChars
// becomes one chunk, so an RSS feed naturally falls apart into items and a
// catalog into its books.
func chunkXMLNode(n *xmlNode, section string, chunks *[]Chunk, ordinal *int) {
	rendered := renderXMLNode(n)
	if utf8.RuneCountInString(rendered) <= maxChunkChars || len(n.children) == 0 {
		emitXMLChunks(rendered, section, chunks, ordinal)
		return
	}

	// Subtree too big: emit the node's own payload, descend into children.
	emitXMLChunks(strings.Join(directXMLLines(n), "\n"), section, chunks, ordinal)
	for _, c := range n.children {
		chunkXMLNode(c, section+" > "+c.name, chunks, ordinal)
	}
}

func emitXMLChunks(text, section string, chunks *[]Chunk, ordinal *int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for _, part := range splitByParagraphs(text) {
		*chunks = append(*chunks, Chunk{Section: section, Text: part, Ordinal: *ordinal})
		*ordinal++
	}
}

// renderXMLNode flattens a subtree into "name: value" lines.
func renderXMLNode(n *xmlNode) string {
	lines := directXMLLines(n)
	var walk func(*xmlNode)
	walk = func(c *xmlNode) {
		for _, a := range c.attrs {
			lines = append(lines, c.name+" "+a.Name.Local+": "+a.Value)
		}
		if t := strings.TrimSpace(c.text.String()); t != "" {
			lines = append(lines, c.name+": "+t)
		}
		for _, gc := range c.children {
			walk(gc)
		}
	}
	for _, c := range n.children {
		walk(c)
	}
	return strings.Join(lines, "\n")
}

// directXMLLines renders only the node's own attributes and text.
func directXMLLines(n *xmlNode) []string {
	var lines []string
	for _, a := range n.attrs {
		lines = append(lines, a.Name.Local+": "+a.Value)
	}
	if t := strings.TrimSpace(n.text.String()); t != "" {
		lines = append(lines, t)
	}
	return lines
}
