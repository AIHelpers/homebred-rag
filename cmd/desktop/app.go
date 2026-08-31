package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"homebred-rag/internal/adapter/embedder"
	"homebred-rag/internal/adapter/index"
	"homebred-rag/internal/adapter/llm"
	"homebred-rag/internal/adapter/parser"
	"homebred-rag/internal/domain"
	"homebred-rag/internal/infra/storage"
	"homebred-rag/internal/usecase"
)

const embeddingDim = 256

// App is the Wails-bound backend: every exported method on this struct is
// callable from the frontend JS as window.go.main.App.<Method>(...). It's a
// thin binding layer only — all real logic lives in internal/usecase, the
// same code path the CLI (cmd/cli) drives. That's the point of the clean
// architecture split in the original plan: the desktop shell is just
// another adapter over the same use cases.
type App struct {
	ctx context.Context

	store     *storage.FileStore
	embedder  usecase.Embedder
	parsers   []usecase.DocumentParser
	ocrEngine parser.OCREngine

	// Live collections, keyed by collection ID, so repeated queries against
	// an already-loaded collection don't re-read/rebuild indices from disk.
	loaded map[string]*loadedCollection
}

type loadedCollection struct {
	store   *domain.CollectionStore
	vector  *index.MemoryVectorIndex
	keyword *index.BM25KeywordIndex
}

func NewApp() *App {
	ocrEngine, ocrAvailable := parser.NewOCREngine()
	pdfParser := parser.PDFParser{}
	parsers := []usecase.DocumentParser{
		parser.MarkdownParser{},
		pdfParser,
		parser.NewCodeParser(),
		parser.TextParser{},
	}
	if ocrAvailable {
		parsers[1] = parser.NewPDFParser(ocrEngine)
		parsers = append(parsers, parser.ImageParser{OCR: ocrEngine})
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	return &App{
		store:     storage.NewFileStore(filepath.Join(home, ".homebredrag", "collections")),
		embedder:  embedder.NewHashingEmbedder(embeddingDim),
		parsers:   parsers,
		ocrEngine: ocrEngine,
		loaded:    map[string]*loadedCollection{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	if a.ocrEngine != nil {
		a.ocrEngine.Close()
	}
}

// --- types returned to the frontend --------------------------------------

// CollectionSummary is what the sidebar collection list is built from.
type CollectionSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"rootPath"`
	DocCount  int    `json:"docCount"`
	ChunkCount int   `json:"chunkCount"`
}

// IndexResult mirrors usecase.IngestResult, JSON-friendly for the frontend.
type IndexResult struct {
	Added     []string          `json:"added"`
	Updated   []string          `json:"updated"`
	Unchanged []string          `json:"unchanged"`
	Skipped   []string          `json:"skipped"`
	Errors    map[string]string `json:"errors"`
	TotalChunks int             `json:"totalChunks"`
}

// AnswerResult mirrors domain.Answer, JSON-friendly for the frontend.
type AnswerResult struct {
	Text       string              `json:"text"`
	Answerable bool                `json:"answerable"`
	Confidence float64             `json:"confidence"`
	Citations  []domain.Citation   `json:"citations"`
	Backend    string              `json:"backend"`
}

// --- frontend-callable methods --------------------------------------------

// OCRAvailable reports whether this build was compiled with OCR support
// (-tags ocr), so the frontend can hide/show scanned-document affordances.
func (a *App) OCRAvailable() bool {
	return a.ocrEngine != nil
}

// ChooseFolder opens a native "select folder" dialog and returns the chosen
// path, or "" if the user cancelled.
func (a *App) ChooseFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder to index",
	})
}

// ListCollections returns every persisted collection with basic stats.
func (a *App) ListCollections() ([]CollectionSummary, error) {
	ids, err := a.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]CollectionSummary, 0, len(ids))
	for _, id := range ids {
		cs, err := a.store.Load(id)
		if err != nil {
			continue
		}
		out = append(out, CollectionSummary{
			ID:         cs.Collection.ID,
			Name:       cs.Collection.Name,
			RootPath:   cs.Collection.RootPath,
			DocCount:   len(cs.Documents),
			ChunkCount: len(cs.Chunks),
		})
	}
	return out, nil
}

// IndexFolder ingests rootPath into the named collection (creating it if
// it doesn't exist yet), persists the result, and returns a summary.
func (a *App) IndexFolder(name, rootPath string) (*IndexResult, error) {
	coll := usecase.NewCollection(name, rootPath)

	lc, err := a.getOrLoad(coll)
	if err != nil {
		return nil, err
	}

	ing := &usecase.Ingester{
		Parsers:  a.parsers,
		Embedder: a.embedder,
		Vector:   lc.vector,
		Keyword:  lc.keyword,
		Chunking: usecase.DefaultChunkOptions(),
	}
	res, err := ing.IngestFolder(lc.store, rootPath)
	if err != nil {
		return nil, err
	}
	if err := a.store.Save(lc.store); err != nil {
		return nil, err
	}

	errs := map[string]string{}
	for path, e := range res.Errors {
		errs[path] = e.Error()
	}
	return &IndexResult{
		Added: res.Added, Updated: res.Updated, Unchanged: res.Unchanged,
		Skipped: res.Skipped, Errors: errs, TotalChunks: len(lc.store.Chunks),
	}, nil
}

// AskQuestion answers a question against the named collection.
func (a *App) AskQuestion(collectionName, question string, topK int) (*AnswerResult, error) {
	coll := usecase.NewCollection(collectionName, "")
	if !a.store.Exists(coll.ID) {
		return nil, fmt.Errorf("no such collection %q — index a folder into it first", collectionName)
	}
	lc, err := a.getOrLoad(coll)
	if err != nil {
		return nil, err
	}

	llmClient := a.llmClient()
	answerer := &usecase.Answerer{
		Vector:   lc.vector,
		Keyword:  lc.keyword,
		Embedder: a.embedder,
		LLM:      llmClient,
		Options:  usecase.DefaultAnswerOptions(),
	}
	ans, err := answerer.AnswerQuestion(domain.Query{CollectionID: coll.ID, Text: question, TopK: topK})
	if err != nil {
		return nil, err
	}
	return &AnswerResult{
		Text: ans.Text, Answerable: ans.Answerable, Confidence: ans.Confidence,
		Citations: ans.Citations, Backend: llmClient.Name(),
	}, nil
}

// ExportSession writes a Q&A exchange to a markdown file the user picks via
// a native save dialog. Returns the chosen path, or "" if cancelled.
func (a *App) ExportSession(collectionName, question string, answer AnswerResult) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Q&A session",
		DefaultFilename: "homebred-rag-session.md",
	})
	if err != nil || path == "" {
		return "", err
	}
	ans := domain.Answer{
		Text: answer.Text, Answerable: answer.Answerable,
		Confidence: answer.Confidence, Citations: answer.Citations,
	}
	md := usecase.ExportSessionMarkdown(collectionName, []usecase.QAExchange{{Question: question, Answer: ans}})
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// --- internals -------------------------------------------------------------

func (a *App) getOrLoad(coll domain.Collection) (*loadedCollection, error) {
	if lc, ok := a.loaded[coll.ID]; ok {
		return lc, nil
	}
	var cs *domain.CollectionStore
	if a.store.Exists(coll.ID) {
		loaded, err := a.store.Load(coll.ID)
		if err != nil {
			return nil, err
		}
		cs = loaded
	} else {
		cs = domain.NewCollectionStore(coll)
	}
	lc := &loadedCollection{
		store:   cs,
		vector:  index.NewMemoryVectorIndex(),
		keyword: index.NewBM25KeywordIndex(),
	}
	usecase.RebuildIndices(cs, lc.vector, lc.keyword)
	a.loaded[coll.ID] = lc
	return lc, nil
}

func (a *App) llmClient() usecase.LLMClient {
	if remote, ok := llm.NewAnthropicClient(); ok {
		return remote
	}
	return llm.ExtractiveClient{}
}
