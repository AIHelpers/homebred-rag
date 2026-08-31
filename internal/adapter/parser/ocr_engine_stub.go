//go:build !ocr

package parser

// NewOCREngine reports that no OCR engine is available in this build.
// Callers should treat (nil, false) as "don't wire up OCR-dependent
// parsers" rather than calling ImageToText on a nil engine.
func NewOCREngine() (OCREngine, bool) {
	return nil, false
}
