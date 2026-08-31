package parser

// OCREngine turns a raster image into plain text. Exactly one
// implementation is compiled in, selected by the `ocr` build tag:
//
//   - `go build ./...`           -> ocr_engine_stub.go: NewOCREngine()
//     returns (nil, false); OCR-dependent parsers (ImageParser, and
//     PDFParser's scanned-PDF fallback) are simply not wired up, so
//     scanned files just get skipped like any other unparseable file.
//     This keeps the default build fully dependency-free.
//
//   - `go build -tags ocr ./...` -> ocr_engine_tesseract.go: NewOCREngine()
//     returns a real Tesseract-backed engine (via github.com/otiai10/
//     gosseract/v2, which needs cgo + tesseract-ocr/libtesseract-dev
//     installed at build time, and the tesseract-ocr runtime + trained
//     language data installed wherever the binary runs).
type OCREngine interface {
	// ImageToText runs OCR on the image at path and returns recognized text.
	ImageToText(path string) (string, error)
	// Close releases any engine resources. Safe to call once, at shutdown.
	Close()
}
