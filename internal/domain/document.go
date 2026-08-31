package domain

import "time"

// Document represents a single ingested source file.
type Document struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	MimeType  string    `json:"mime_type"`
	IndexedAt time.Time `json:"indexed_at"`
}

// Chunk is a slice of a Document, with its embedding once computed.
type Chunk struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"document_id"`
	DocumentPath string   `json:"document_path"`
	Text        string    `json:"text"`
	StartOffset int       `json:"start_offset"`
	EndOffset   int       `json:"end_offset"`
	PageNumber  *int      `json:"page_number,omitempty"`
	Embedding   []float32 `json:"embedding,omitempty"`
}

// Collection is an independent knowledge base ("library").
type Collection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
}

// Query is a user question against a Collection.
type Query struct {
	CollectionID string
	Text         string
	TopK         int
}

// Citation points back to the chunk an Answer drew from.
type Citation struct {
	ChunkID      string `json:"chunk_id"`
	DocumentPath string `json:"document_path"`
	Snippet      string `json:"snippet"`
	Score        float64 `json:"score"`
}

// Answer is the grounded response to a Query.
type Answer struct {
	Text       string     `json:"text"`
	Citations  []Citation `json:"citations"`
	Confidence float64    `json:"confidence"`
	Answerable bool       `json:"answerable"`
}
