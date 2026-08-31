//go:build ocr

package parser

import "github.com/otiai10/gosseract/v2"

// tesseractEngine wraps a gosseract client. gosseract binds to the system
// Tesseract OCR library via cgo, so this file only compiles with `-tags
// ocr`, and needs tesseract-ocr + libtesseract-dev (or equivalent) present
// wherever it's built, and the tesseract-ocr runtime installed wherever the
// resulting binary runs.
type tesseractEngine struct {
	client *gosseract.Client
}

// NewOCREngine returns a working Tesseract-backed OCR engine.
func NewOCREngine() (OCREngine, bool) {
	return &tesseractEngine{client: gosseract.NewClient()}, true
}

func (t *tesseractEngine) ImageToText(path string) (string, error) {
	if err := t.client.SetImage(path); err != nil {
		return "", err
	}
	return t.client.Text()
}

func (t *tesseractEngine) Close() {
	_ = t.client.Close()
}
