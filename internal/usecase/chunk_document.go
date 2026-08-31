package usecase

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"homebred-rag/internal/domain"
)

// ChunkOptions configures the fixed-size + overlap chunking strategy.
type ChunkOptions struct {
	SizeWords    int // target chunk size, in words
	OverlapWords int // words of overlap between consecutive chunks
}

func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{SizeWords: 220, OverlapWords: 40}
}

// ChunkText splits text into overlapping word-window chunks and returns
// domain.Chunk values (without embeddings — that's a separate step).
// StartOffset/EndOffset are character offsets into the original text,
// so citation UIs can jump to the exact spot in the source file.
func ChunkText(doc domain.Document, text string, opts ChunkOptions) []domain.Chunk {
	if opts.SizeWords <= 0 {
		opts = DefaultChunkOptions()
	}
	if opts.OverlapWords >= opts.SizeWords {
		opts.OverlapWords = opts.SizeWords / 4
	}

	// Tokenize into words while tracking each word's byte offset in `text`.
	type wordSpan struct{ start, end int }
	var spans []wordSpan
	inWord := false
	wordStart := 0
	for i, r := range text {
		isSpace := r == ' ' || r == '\n' || r == '\t' || r == '\r'
		if !isSpace && !inWord {
			inWord = true
			wordStart = i
		} else if isSpace && inWord {
			spans = append(spans, wordSpan{wordStart, i})
			inWord = false
		}
	}
	if inWord {
		spans = append(spans, wordSpan{wordStart, len(text)})
	}
	if len(spans) == 0 {
		return nil
	}

	step := opts.SizeWords - opts.OverlapWords
	if step <= 0 {
		step = opts.SizeWords
	}

	var chunks []domain.Chunk
	idx := 0
	for start := 0; start < len(spans); start += step {
		end := start + opts.SizeWords
		if end > len(spans) {
			end = len(spans)
		}
		startOffset := spans[start].start
		endOffset := spans[end-1].end
		chunkText := strings.TrimSpace(text[startOffset:endOffset])
		if chunkText != "" {
			chunks = append(chunks, domain.Chunk{
				ID:           chunkID(doc.ID, idx),
				DocumentID:   doc.ID,
				DocumentPath: doc.Path,
				Text:         chunkText,
				StartOffset:  startOffset,
				EndOffset:    endOffset,
			})
			idx++
		}
		if end == len(spans) {
			break
		}
	}
	return chunks
}

func chunkID(docID string, idx int) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s:%d", docID, idx)))
	return hex.EncodeToString(h[:])[:16]
}
