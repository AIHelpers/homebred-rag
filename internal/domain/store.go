package domain

// CollectionStore is the full persisted state of one Collection:
// its metadata, the documents ingested into it, and every chunk
// (with embedding) produced from those documents. This is the
// unit that gets marshalled to disk by infra/storage.
type CollectionStore struct {
	Collection Collection            `json:"collection"`
	Documents  map[string]Document   `json:"documents"`   // keyed by Document.ID (= path hash)
	Chunks     map[string]Chunk      `json:"chunks"`       // keyed by Chunk.ID
}

func NewCollectionStore(c Collection) *CollectionStore {
	return &CollectionStore{
		Collection: c,
		Documents:  map[string]Document{},
		Chunks:     map[string]Chunk{},
	}
}

// ChunksForDocument returns every chunk belonging to a document.
func (s *CollectionStore) ChunksForDocument(docID string) []Chunk {
	var out []Chunk
	for _, c := range s.Chunks {
		if c.DocumentID == docID {
			out = append(out, c)
		}
	}
	return out
}

// RemoveDocument deletes a document and all its chunks (used before re-indexing a changed file).
func (s *CollectionStore) RemoveDocument(docID string) {
	delete(s.Documents, docID)
	for id, c := range s.Chunks {
		if c.DocumentID == docID {
			delete(s.Chunks, id)
		}
	}
}
