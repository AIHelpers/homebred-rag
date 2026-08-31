package index

import (
	"math"
	"sort"
	"sync"

	"homebred-rag/internal/domain"
	"homebred-rag/internal/usecase"
)

// MemoryVectorIndex is a brute-force cosine-similarity nearest-neighbour
// index. It's the pure-Go fallback the plan calls for when the cgo
// sqlite-vec / HNSW route adds friction (see tech stack section 5) — at
// collection sizes typical of a single user's documents (thousands of
// chunks), brute force is fast enough that an approximate index isn't
// worth the added complexity. Swapping in a real HNSW graph later only
// means writing a new adapter behind usecase.VectorIndex.
type MemoryVectorIndex struct {
	mu     sync.RWMutex
	chunks map[string]domain.Chunk
}

func NewMemoryVectorIndex() *MemoryVectorIndex {
	return &MemoryVectorIndex{chunks: map[string]domain.Chunk{}}
}

func (idx *MemoryVectorIndex) Add(c domain.Chunk) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.chunks[c.ID] = c
}

func (idx *MemoryVectorIndex) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.chunks, id)
}

func (idx *MemoryVectorIndex) Search(query []float32, topK int) []usecase.ScoredChunk {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	results := make([]usecase.ScoredChunk, 0, len(idx.chunks))
	for _, c := range idx.chunks {
		results = append(results, usecase.ScoredChunk{
			Chunk: c,
			Score: cosineSimilarity(query, c.Embedding),
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
