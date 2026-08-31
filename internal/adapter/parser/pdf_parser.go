package parser

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PDFParser is a minimal, dependency-free PDF text extractor: it finds each
// page's content stream, inflates it if FlateDecode-compressed, and pulls
// text out of Tj/TJ show-text operators. It won't handle exotic encodings
// or embedded fonts with custom glyph maps, but it handles the common case
// of PDFs exported from a word processor or "print to PDF".
//
// Scanned (image-only) PDFs have no extractable text stream at all, so direct
// extraction alone can't help there. If OCR is configured (see NewOCREngine
// in ocr_engine_tesseract.go / ocr_engine_stub.go, selected by the `ocr`
// build tag), PDFParser falls back to rasterizing each page with `pdftoppm`
// (from poppler-utils) and running OCR over the page images whenever direct
// extraction comes back too sparse to be useful. With no OCR engine
// configured (the default, dependency-free build), scanned PDFs simply
// extract as empty/near-empty text, same as before this feature existed.
type PDFParser struct {
	// OCR is optional. Leave nil to disable the scanned-PDF fallback
	// entirely (default, dependency-free behavior).
	OCR OCREngine
	// MinTextChars: if direct extraction yields fewer than this many
	// non-whitespace characters, the PDF is treated as likely-scanned and
	// the OCR fallback is attempted (if OCR is configured). Defaults to 80.
	MinTextChars int
	// RasterDPI controls the resolution pages are rasterized at before
	// OCR. Higher is more accurate but slower. Defaults to 200.
	RasterDPI int
}

// NewPDFParser builds a PDFParser with OCR fallback wired in. Pass a nil
// engine (or use the zero-value PDFParser{}) to get pure direct-extraction
// behavior with no OCR fallback.
func NewPDFParser(ocr OCREngine) PDFParser {
	return PDFParser{OCR: ocr, MinTextChars: 80, RasterDPI: 200}
}

func (PDFParser) CanParse(path string) bool {
	return filepath.Ext(path) == ".pdf"
}

func (p PDFParser) Parse(path string) (string, []int, error) {
	text, pageBoundaries, err := p.parseDirect(path)
	if err != nil {
		return "", nil, err
	}

	if p.OCR == nil || len(strings.TrimSpace(text)) >= p.minTextChars() {
		return text, pageBoundaries, nil
	}

	// Direct extraction came back (near-)empty and an OCR engine is
	// configured: this is very likely a scanned/image-only PDF. Fall back
	// to rasterizing pages and OCRing them, but only if pdftoppm is
	// actually on PATH — otherwise degrade gracefully to whatever direct
	// extraction produced rather than erroring the whole ingest.
	if _, lookErr := exec.LookPath("pdftoppm"); lookErr != nil {
		return text, pageBoundaries, nil
	}
	ocrText, ocrBoundaries, ocrErr := p.parseViaOCR(path)
	if ocrErr != nil || strings.TrimSpace(ocrText) == "" {
		// OCR fallback didn't pan out; return whatever direct extraction had.
		return text, pageBoundaries, nil
	}
	return ocrText, ocrBoundaries, nil
}

func (p PDFParser) minTextChars() int {
	if p.MinTextChars <= 0 {
		return 80
	}
	return p.MinTextChars
}

func (p PDFParser) rasterDPI() int {
	if p.RasterDPI <= 0 {
		return 200
	}
	return p.RasterDPI
}

func (p PDFParser) parseDirect(path string) (string, []int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	streams := extractStreams(raw)

	var b strings.Builder
	var pageBoundaries []int
	for _, s := range streams {
		before := b.Len()
		text := extractTextFromContentStream(s)
		if text != "" {
			if before > 0 {
				pageBoundaries = append(pageBoundaries, before)
			}
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	}
	return b.String(), pageBoundaries, nil
}

var streamRe = regexp.MustCompile(`(?s)<<(.*?)>>\s*stream\r?\n(.*?)\r?\nendstream`)

// extractStreams pulls every PDF object stream out of the raw file bytes,
// inflating it first if its dictionary declares /Filter /FlateDecode.
func extractStreams(raw []byte) [][]byte {
	var out [][]byte
	for _, m := range streamRe.FindAllSubmatch(raw, -1) {
		dict := m[1]
		data := m[2]
		if bytes.Contains(dict, []byte("/FlateDecode")) {
			if inflated, err := inflate(data); err == nil {
				data = inflated
			} else {
				continue
			}
		}
		// Only keep streams that look like content streams (contain a text
		// block), skipping images, fonts, xref streams, etc.
		if bytes.Contains(data, []byte("BT")) && bytes.Contains(data, []byte("Tj")) ||
			bytes.Contains(data, []byte("TJ")) {
			out = append(out, data)
		}
	}
	return out
}

func inflate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

var (
	tjStringRe  = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\)\s*Tj`)
	tjArrayRe   = regexp.MustCompile(`\[((?:[^\[\]]|\\.)*)\]\s*TJ`)
	arrayPartRe = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\)`)
)

// extractTextFromContentStream pulls literal strings out of Tj and TJ
// operators, which is how PDF content streams encode "draw this text".
func extractTextFromContentStream(stream []byte) string {
	var b strings.Builder
	for _, m := range tjStringRe.FindAllSubmatch(stream, -1) {
		b.WriteString(unescapePDFString(string(m[1])))
		b.WriteString(" ")
	}
	for _, m := range tjArrayRe.FindAllSubmatch(stream, -1) {
		for _, p := range arrayPartRe.FindAllSubmatch(m[1], -1) {
			b.WriteString(unescapePDFString(string(p[1])))
		}
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

func unescapePDFString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case 'n':
				b.WriteByte('\n')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case '(', ')', '\\':
				b.WriteByte(next)
				i++
			default:
				// Octal escape \ddd
				if next >= '0' && next <= '7' && i+3 < len(s) {
					if v, err := strconv.ParseInt(s[i+1:i+4], 8, 32); err == nil {
						b.WriteByte(byte(v))
						i += 3
						continue
					}
				}
				b.WriteByte(next)
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseViaOCR rasterizes each page of the PDF at pdfPath (via the external
// `pdftoppm` binary) and runs the configured OCR engine over each page
// image, concatenating the results. pageBoundaries marks the character
// offset in the returned text where each new page's OCR text begins, so
// citations can still report an approximate page number.
func (p PDFParser) parseViaOCR(pdfPath string) (string, []int, error) {
	tmpDir, err := os.MkdirTemp("", "homebredrag-ocr-*")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmpDir)

	pages, err := rasterizePDF(pdfPath, tmpDir, p.rasterDPI())
	if err != nil {
		return "", nil, err
	}
	if len(pages) == 0 {
		return "", nil, fmt.Errorf("pdftoppm produced no page images for %s", pdfPath)
	}

	var b strings.Builder
	var boundaries []int
	for _, imgPath := range pages {
		pageText, err := p.OCR.ImageToText(imgPath)
		if err != nil {
			continue // skip unreadable pages rather than failing the whole document
		}
		pageText = strings.TrimSpace(pageText)
		if pageText == "" {
			continue
		}
		if b.Len() > 0 {
			boundaries = append(boundaries, b.Len())
		}
		b.WriteString(pageText)
		b.WriteString("\n\n")
	}
	return b.String(), boundaries, nil
}

// rasterizePDF shells out to `pdftoppm` (part of poppler-utils) to render
// every page of pdfPath to a PNG in outDir, and returns the resulting image
// paths in page order.
func rasterizePDF(pdfPath, outDir string, dpi int) ([]string, error) {
	prefix := filepath.Join(outDir, "page")
	cmd := exec.Command("pdftoppm", "-png", "-r", strconv.Itoa(dpi), pdfPath, prefix)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftoppm failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil, err
	}

	type pageFile struct {
		num  int
		path string
	}
	var pages []pageFile
	pageNumRe := regexp.MustCompile(`-(\d+)\.png$`)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := pageNumRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		pages = append(pages, pageFile{num: num, path: filepath.Join(outDir, e.Name())})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].num < pages[j].num })

	out := make([]string, len(pages))
	for i, pg := range pages {
		out[i] = pg.path
	}
	return out, nil
}
