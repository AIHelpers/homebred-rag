package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"homebred-rag/internal/domain"
)

// IngestResult summarizes what happened during an ingest run.
type IngestResult struct {
	Added     []string
	Updated   []string
	Unchanged []string
	Skipped   []string // files no parser could handle
	Errors    map[string]error
}

// Ingester wires together the parsers/embedder needed to turn files on disk
// into indexed chunks inside a CollectionStore, plus the two search indices.
type Ingester struct {
	Parsers  []DocumentParser
	Embedder Embedder
	Vector   VectorIndex
	Keyword  KeywordIndex
	Chunking ChunkOptions
}

// IngestFolder recursively walks root, parses every file a registered parser
// can handle, chunks + embeds it, and adds/updates it in store. Files whose
// content hash hasn't changed since the last run are skipped entirely
// (incremental re-embedding), and files that were removed from disk since
// the last run are removed from the store and both indices.
func (ig *Ingester) IngestFolder(store *domain.CollectionStore, root string) (*IngestResult, error) {
	res := &IngestResult{Errors: map[string]error{}}
	seen := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		parser := ig.findParser(path)
		if parser == nil {
			res.Skipped = append(res.Skipped, path)
			return nil
		}

		hash, herr := hashFile(path)
		if herr != nil {
			res.Errors[path] = herr
			return nil
		}
		docID := docIDFor(path)
		seen[docID] = true

		if existing, ok := store.Documents[docID]; ok && existing.Hash == hash {
			res.Unchanged = append(res.Unchanged, path)
			return nil
		}

		text, pages, perr := parser.Parse(path)
		if perr != nil {
			res.Errors[path] = perr
			return nil
		}

		// Replace any prior version of this document.
		wasPresent := false
		if _, ok := store.Documents[docID]; ok {
			wasPresent = true
			for _, old := range store.ChunksForDocument(docID) {
				ig.Vector.Remove(old.ID)
				ig.Keyword.Remove(old.ID)
			}
			store.RemoveDocument(docID)
		}

		doc := domain.Document{
			ID:        docID,
			Path:      path,
			Hash:      hash,
			MimeType:  mimeTypeFor(path),
			IndexedAt: time.Now(),
		}
		store.Documents[docID] = doc

		chunks := ChunkText(doc, text, ig.Chunking)
		if len(pages) > 0 {
			assignPageNumbers(chunks, pages)
		}
		chunks = EmbedChunks(ig.Embedder, chunks)
		for _, c := range chunks {
			store.Chunks[c.ID] = c
			ig.Vector.Add(c)
			ig.Keyword.Add(c)
		}

		if wasPresent {
			res.Updated = append(res.Updated, path)
		} else {
			res.Added = append(res.Added, path)
		}
		return nil
	})
	if err != nil {
		return res, err
	}

	// Anything previously indexed under this root but no longer on disk: drop it.
	for id := range store.Documents {
		if !seen[id] {
			for _, old := range store.ChunksForDocument(id) {
				ig.Vector.Remove(old.ID)
				ig.Keyword.Remove(old.ID)
			}
			store.RemoveDocument(id)
		}
	}

	return res, nil
}

func (ig *Ingester) findParser(path string) DocumentParser {
	for _, p := range ig.Parsers {
		if p.CanParse(path) {
			return p
		}
	}
	return nil
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func docIDFor(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}

func mimeTypeFor(path string) string {
	switch filepath.Ext(path) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".tiff", ".tif":
		return "image/tiff"
	case ".bmp":
		return "image/bmp"
	default:
		return "text/plain; variant=code"
	}
}

// assignPageNumbers stamps each chunk with the page it starts on, given a
// sorted slice of character offsets where each new page begins.
func assignPageNumbers(chunks []domain.Chunk, pageBoundaries []int) {
	for i := range chunks {
		page := 1
		for _, boundary := range pageBoundaries {
			if chunks[i].StartOffset >= boundary {
				page++
			} else {
				break
			}
		}
		p := page
		chunks[i].PageNumber = &p
	}
}
