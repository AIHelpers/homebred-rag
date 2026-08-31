package usecase

import "homebred-rag/internal/domain"

// EmbedChunks fills in the Embedding field of every chunk in place, using
// the given Embedder. Kept as its own step (rather than folded into
// chunking) so embedding models can be swapped or re-run independently
// (e.g. incremental re-embedding after a model upgrade).
func EmbedChunks(embedder Embedder, chunks []domain.Chunk) []domain.Chunk {
	out := make([]domain.Chunk, len(chunks))
	for i, c := range chunks {
		c.Embedding = embedder.Embed(c.Text)
		out[i] = c
	}
	return out
}
