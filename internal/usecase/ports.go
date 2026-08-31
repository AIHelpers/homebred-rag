package usecase

import "homebred-rag/internal/domain"

// DocumentParser turns a raw file on disk into plain text ready for chunking.
// Different implementations handle markdown, plain text/code, PDF, etc.
type DocumentParser interface {
	// CanParse reports whether this parser handles the given file (by extension/content).
	CanParse(path string) bool
	// Parse extracts plain text (and optional per-page boundaries) from the file.
	Parse(path string) (text string, pageBoundaries []int, err error)
}

// Embedder turns text into a fixed-size vector, entirely locally.
type Embedder interface {
	Embed(text string) []float32
	Dim() int
}

// VectorIndex supports nearest-neighbour search over chunk embeddings.
type VectorIndex interface {
	Add(chunk domain.Chunk)
	Remove(chunkID string)
	Search(query []float32, topK int) []ScoredChunk
}

// KeywordIndex supports BM25-ish exact-term search over chunk text.
type KeywordIndex interface {
	Add(chunk domain.Chunk)
	Remove(chunkID string)
	Search(query string, topK int) []ScoredChunk
}

// LLMClient turns retrieved context + a question into a natural-language answer.
// Implementations may be fully local (extractive) or call a remote API.
type LLMClient interface {
	Answer(question string, context []domain.Chunk) (string, error)
	Name() string
}

// ScoredChunk pairs a chunk with a similarity/relevance score.
type ScoredChunk struct {
	Chunk domain.Chunk
	Score float64
}
