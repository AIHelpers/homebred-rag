//go:build ocr

package parser_test

import (
	"os/exec"
	"strings"
	"testing"

	"homebred-rag/internal/adapter/parser"
)

// These tests exercise the real Tesseract-backed OCR path end to end. They
// only run when built with `-tags ocr` (see ocr_engine_tesseract.go) and
// skip gracefully if `pdftoppm` isn't on PATH, since the scanned-PDF
// fallback shells out to it.
func requirePdftoppm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not on PATH (install poppler-utils) — skipping OCR/PDF fallback test")
	}
}

func TestImageParser_OCRsScannedImage(t *testing.T) {
	engine, available := parser.NewOCREngine()
	if !available {
		t.Fatal("expected a real OCR engine when built with -tags ocr")
	}
	defer engine.Close()

	p := parser.ImageParser{OCR: engine}
	if !p.CanParse("testdata/scanned-receipt.png") {
		t.Fatal("expected ImageParser to claim .png files when OCR is configured")
	}

	text, _, err := p.Parse("testdata/scanned-receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "revenue") || !strings.Contains(text, "42000") {
		t.Fatalf("expected OCR to recognize the receipt text, got %q", text)
	}
}

func TestPDFParser_FallsBackToOCRForScannedPDF(t *testing.T) {
	requirePdftoppm(t)

	engine, available := parser.NewOCREngine()
	if !available {
		t.Fatal("expected a real OCR engine when built with -tags ocr")
	}
	defer engine.Close()

	p := parser.NewPDFParser(engine)

	// Sanity check: this fixture has no extractable text layer (it's a
	// Pillow-saved image-only PDF), so a plain PDFParser{} gets nothing —
	// which is exactly the condition that should trigger the OCR fallback.
	direct := parser.PDFParser{}
	directText, _, err := direct.Parse("testdata/scanned.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(directText) != "" {
		t.Fatalf("fixture is expected to have no direct text layer, got %q", directText)
	}

	text, _, err := p.Parse("testdata/scanned.pdf")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "revenue") || !strings.Contains(text, "42000") {
		t.Fatalf("expected OCR fallback to recognize the scanned page text, got %q", text)
	}
}

func TestPDFParser_NoOCRFallbackWhenEngineNil(t *testing.T) {
	// Without an OCR engine configured, a scanned PDF should just come back
	// empty (or whatever direct extraction found), never error — this is
	// the default (non -tags ocr) behavior, verified here too since we
	// have the real fixture in this build.
	p := parser.PDFParser{}
	text, _, err := p.Parse("testdata/scanned.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) != "" {
		t.Fatalf("expected empty text with no OCR engine configured, got %q", text)
	}
}
