package parser

import (
	"os"
	"strings"
	"testing"
)

func TestMarkdownParser_CanParse(t *testing.T) {
	p := MarkdownParser{}
	if !p.CanParse("notes.md") || !p.CanParse("notes.markdown") {
		t.Fatal("expected .md/.markdown to be claimed")
	}
	if p.CanParse("notes.txt") {
		t.Fatal("should not claim .txt")
	}
}

func TestMarkdownParser_StripsMarkupKeepsProse(t *testing.T) {
	p := MarkdownParser{}
	text, _, err := p.Parse("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "**") || strings.Contains(text, "##") {
		t.Fatalf("expected markup stripped, got: %q", text)
	}
	if !strings.Contains(text, "Title Heading") || !strings.Contains(text, "Plain prose continues") {
		t.Fatalf("expected prose preserved, got: %q", text)
	}
}

func TestMarkdownParser_OffsetsAlignToOriginalFile(t *testing.T) {
	p := MarkdownParser{}
	text, _, err := p.Parse("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != len(orig) {
		t.Fatalf("stripped text must keep original byte length so offsets stay valid: got %d want %d", len(text), len(orig))
	}
}

func TestTextParser(t *testing.T) {
	p := TextParser{}
	if !p.CanParse("a.txt") {
		t.Fatal("should claim .txt")
	}
	text, _, err := p.Parse("testdata/sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Plain text file content") {
		t.Fatalf("expected verbatim content, got %q", text)
	}
}

func TestCodeParser(t *testing.T) {
	p := NewCodeParser()
	if !p.CanParse("main.go") || !p.CanParse("app.py") {
		t.Fatal("should claim known source extensions")
	}
	if p.CanParse("readme.md") {
		t.Fatal("should not claim markdown")
	}
	text, _, err := p.Parse("testdata/sample.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "func Add(a, b int) int") {
		t.Fatalf("expected verbatim source, got %q", text)
	}
}

