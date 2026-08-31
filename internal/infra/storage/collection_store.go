package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"homebred-rag/internal/domain"
)

// FileStore persists each collection's chunks/embeddings/metadata as a
// single JSON file under a base directory. It plays the role the plan's
// section 5 gives to SQLite (+ sqlite-vec) — collection metadata and
// vector storage — without a cgo dependency, which this sandboxed build
// environment can't fetch. Swapping in real SQLite later is a matter of
// writing a new adapter behind the same load/save calls; nothing in
// usecase/ or domain/ knows or cares how a CollectionStore is persisted.
type FileStore struct {
	BaseDir string
}

func NewFileStore(baseDir string) *FileStore {
	return &FileStore{BaseDir: baseDir}
}

func (fs *FileStore) path(collectionID string) string {
	return filepath.Join(fs.BaseDir, collectionID+".json")
}

func (fs *FileStore) Load(collectionID string) (*domain.CollectionStore, error) {
	b, err := os.ReadFile(fs.path(collectionID))
	if err != nil {
		return nil, err
	}
	var store domain.CollectionStore
	if err := json.Unmarshal(b, &store); err != nil {
		return nil, fmt.Errorf("corrupt collection store %s: %w", collectionID, err)
	}
	return &store, nil
}

func (fs *FileStore) Exists(collectionID string) bool {
	_, err := os.Stat(fs.path(collectionID))
	return err == nil
}

func (fs *FileStore) Save(store *domain.CollectionStore) error {
	if err := os.MkdirAll(fs.BaseDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp := fs.path(store.Collection.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fs.path(store.Collection.ID))
}

// List returns the IDs of every collection persisted under BaseDir.
func (fs *FileStore) List() ([]string, error) {
	entries, err := os.ReadDir(fs.BaseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, e.Name()[:len(e.Name())-len(".json")])
		}
	}
	return ids, nil
}
