package parser

import (
	"path/filepath"
	"strings"
)

// ImageParser handles standalone scanned images (a photographed receipt, a
// screenshot of a printed page, etc.) by running them through the
// configured OCREngine. If OCR is nil (the default, non-`ocr`-tagged
// build), CanParse always reports false, so these files are simply skipped
// during ingestion rather than erroring — the same as any other file type
// with no registered parser.
type ImageParser struct {
	OCR OCREngine
}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".tiff": true, ".tif": true, ".bmp": true,
}

func (p ImageParser) CanParse(path string) bool {
	if p.OCR == nil {
		return false
	}
	return imageExtensions[strings.ToLower(filepath.Ext(path))]
}

func (p ImageParser) Parse(path string) (string, []int, error) {
	text, err := p.OCR.ImageToText(path)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(text), nil, nil
}
