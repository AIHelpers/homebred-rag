package usecase

import (
	"crypto/sha256"
	"encoding/hex"

	"homebred-rag/internal/domain"
)

// NewCollection builds a Collection with a stable, deterministic ID derived
// from its name (so re-running `index --collection foo` always resolves to
// the same collection instead of minting duplicates).
func NewCollection(name, rootPath string) domain.Collection {
	sum := sha256.Sum256([]byte(name))
	return domain.Collection{
		ID:       hex.EncodeToString(sum[:])[:16],
		Name:     name,
		RootPath: rootPath,
	}
}

// RebuildIndices replays every chunk already in a loaded store into fresh
// vector + keyword indices. Needed because those indices are in-memory and
// don't get persisted themselves — only the chunks/embeddings are.
func RebuildIndices(store *domain.CollectionStore, vector VectorIndex, keyword KeywordIndex) {
	for _, c := range store.Chunks {
		vector.Add(c)
		keyword.Add(c)
	}
}
