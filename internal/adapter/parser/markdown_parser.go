package parser

import (
	"os"
	"path/filepath"
	"regexp"
)

// MarkdownParser reads .md/.markdown files. It does a light-touch strip of
// the most common markup (headers, emphasis, links) so embeddings key off
// prose rather than syntax, while leaving offsets referring to the ORIGINAL
// file content (citations should point at what the user actually sees).
type MarkdownParser struct{}

func (MarkdownParser) CanParse(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".md" || ext == ".markdown"
}

func (MarkdownParser) Parse(path string) (string, []int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	// Keep offsets aligned to the raw file: only lightly normalize markup
	// characters to spaces rather than deleting them (deleting would shift
	// every downstream offset and break citation jump-to-source).
	text := string(b)
	text = mdSyntaxNoise.ReplaceAllStringFunc(text, func(m string) string {
		return string(make([]byte, len(m)))
	})
	// Replace the zero-bytes produced above with spaces (keeps byte length identical).
	out := []byte(text)
	for i, c := range out {
		if c == 0 {
			out[i] = ' '
		}
	}
	return string(out), nil, nil
}

// Matches markdown syntax tokens we don't want influencing embeddings:
// heading hashes, emphasis markers, link/image brackets, code fences.
var mdSyntaxNoise = regexp.MustCompile("(?m)(^#{1,6}\\s|[*_`]{1,3}|!?\\[|\\]\\([^)]*\\)|^```[a-zA-Z]*$)")
