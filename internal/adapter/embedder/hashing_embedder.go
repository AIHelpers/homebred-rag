package embedder

import (
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

// HashingEmbedder is a dependency-free, fully local "embedding model": it
// tokenizes text and feature-hashes tokens (unigrams + bigrams) into a
// fixed-size vector (the classic hashing trick), then L2-normalizes it so
// cosine similarity behaves sensibly. It captures lexical/semantic overlap
// well enough for retrieval without shipping or downloading an ML model.
//
// This is the swap-in target for milestone 2/5's real embedding model
// (bge-small/e5-small via ONNX runtime, per the plan's tech stack) — it
// satisfies the same usecase.Embedder interface, so upgrading later means
// writing a new adapter, not touching any usecase code.
type HashingEmbedder struct {
	dim int
}

func NewHashingEmbedder(dim int) *HashingEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &HashingEmbedder{dim: dim}
}

func (h *HashingEmbedder) Dim() int { return h.dim }

var tokenRe = regexp.MustCompile(`[A-Za-z0-9_]+`)

func (h *HashingEmbedder) Embed(text string) []float32 {
	tokens := tokenRe.FindAllString(strings.ToLower(text), -1)
	vec := make([]float64, h.dim)

	addToken := func(tok string) {
		idx := hashToIndex(tok, h.dim)
		vec[idx] += 1.0
	}

	for i, tok := range tokens {
		addToken(tok)
		if i > 0 {
			addToken(tokens[i-1] + "_" + tok) // bigram, catches multi-word terms/IDs
		}
	}

	// log-scale term frequency (dampens very repetitive chunks) then L2 normalize.
	var norm float64
	out := make([]float32, h.dim)
	for i, v := range vec {
		if v > 0 {
			v = 1.0 + math.Log(v)
		}
		norm += v * v
		out[i] = float32(v)
	}
	if norm > 0 {
		inv := float32(1.0 / math.Sqrt(norm))
		for i := range out {
			out[i] *= inv
		}
	}
	return out
}

func hashToIndex(s string, dim int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % uint32(dim))
}
