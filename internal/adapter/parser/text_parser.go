package parser

import (
	"os"
	"path/filepath"
)

// TextParser handles plain text files verbatim (.txt and anything with no
// more specific parser registered before it).
type TextParser struct{}

func (TextParser) CanParse(path string) bool {
	return filepath.Ext(path) == ".txt"
}

func (TextParser) Parse(path string) (string, []int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	return string(b), nil, nil
}

// CodeParser handles source files: read verbatim, since identifiers and
// comments are exactly what we want embedded/keyword-searched as-is.
type CodeParser struct {
	Extensions map[string]bool
}

func NewCodeParser() *CodeParser {
	exts := []string{
		".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".h", ".cpp",
		".hpp", ".rs", ".rb", ".php", ".cs", ".sh", ".sql", ".yaml", ".yml",
		".json", ".toml", ".proto",
	}
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		m[e] = true
	}
	return &CodeParser{Extensions: m}
}

func (c *CodeParser) CanParse(path string) bool {
	return c.Extensions[filepath.Ext(path)]
}

func (c *CodeParser) Parse(path string) (string, []int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	return string(b), nil, nil
}
