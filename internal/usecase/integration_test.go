package usecase_test

import (
	"os"
	"path/filepath"
	"testing"

	"homebred-rag/internal/adapter/embedder"
	"homebred-rag/internal/adapter/index"
	"homebred-rag/internal/adapter/llm"
	"homebred-rag/internal/adapter/parser"
	"homebred-rag/internal/domain"
	"homebred-rag/internal/infra/storage"
	"homebred-rag/internal/usecase"
)

// TestFullPipeline_IngestPersistReloadQuery exercises the whole flow the
// CLI drives: write files to disk -> ingest into a collection -> persist to
// disk -> reload into fresh in-memory indices (as a new process would) ->
// ask a question -> get a grounded, cited answer. This is the integration
// test called for in the plan (section 12), run against a temp directory
// standing in for the temp SQLite DB the plan describes.
func TestFullPipeline_IngestPersistReloadQuery(t *testing.T) {
	srcDir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(srcDir, "policy.md"), []byte(
		"# Remote Work Policy\n\nEmployees may work remotely up to 3 days per week. "+
			"Requests for full-time remote work go through your manager and HR.\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(srcDir, "notes.txt"), []byte(
		"Standup is at 9:30am daily. Retro is every other Friday at 2pm.\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(srcDir, "ignored.png"), []byte("not a real image, just unparseable bytes"), 0o644))

	storeDir := t.TempDir()
	fileStore := storage.NewFileStore(storeDir)
	emb := embedder.NewHashingEmbedder(256)

	// --- ingest ---
	vec1 := index.NewMemoryVectorIndex()
	kw1 := index.NewBM25KeywordIndex()
	coll := usecase.NewCollection("policies", srcDir)
	cs := domain.NewCollectionStore(coll)

	ing := &usecase.Ingester{
		Parsers:  []usecase.DocumentParser{parser.MarkdownParser{}, parser.TextParser{}, parser.NewCodeParser()},
		Embedder: emb,
		Vector:   vec1,
		Keyword:  kw1,
		Chunking: usecase.ChunkOptions{SizeWords: 100, OverlapWords: 10},
	}
	res, err := ing.IngestFolder(cs, srcDir)
	must(t, err)

	if len(res.Added) != 2 {
		t.Fatalf("expected 2 files added (md + txt), got %d: %v", len(res.Added), res.Added)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("expected 1 file skipped (no parser for .png), got %d", len(res.Skipped))
	}
	if len(cs.Chunks) == 0 {
		t.Fatal("expected chunks to be produced")
	}

	must(t, fileStore.Save(cs))

	// --- re-running ingest on unchanged files should be a no-op ---
	res2, err := ing.IngestFolder(cs, srcDir)
	must(t, err)
	if len(res2.Added)+len(res2.Updated) != 0 {
		t.Fatalf("expected no changes on second ingest, got added=%v updated=%v", res2.Added, res2.Updated)
	}
	if len(res2.Unchanged) != 2 {
		t.Fatalf("expected both files reported unchanged, got %d", len(res2.Unchanged))
	}

	// --- reload from disk into fresh indices, as a new process would ---
	loaded, err := fileStore.Load(coll.ID)
	must(t, err)
	if len(loaded.Chunks) != len(cs.Chunks) {
		t.Fatalf("reloaded chunk count mismatch: got %d want %d", len(loaded.Chunks), len(cs.Chunks))
	}

	vec2 := index.NewMemoryVectorIndex()
	kw2 := index.NewBM25KeywordIndex()
	usecase.RebuildIndices(loaded, vec2, kw2)

	answerer := &usecase.Answerer{
		Vector:   vec2,
		Keyword:  kw2,
		Embedder: emb,
		LLM:      llm.ExtractiveClient{},
		Options:  usecase.DefaultAnswerOptions(),
	}
	ans, err := answerer.AnswerQuestion(domain.Query{Text: "How many days a week can I work remotely?", TopK: 3})
	must(t, err)

	if !ans.Answerable {
		t.Fatalf("expected an answerable result, got confidence=%.3f text=%q", ans.Confidence, ans.Text)
	}
	if len(ans.Citations) == 0 {
		t.Fatal("expected at least one citation")
	}
	foundPolicy := false
	for _, c := range ans.Citations {
		if filepath.Base(c.DocumentPath) == "policy.md" {
			foundPolicy = true
		}
	}
	if !foundPolicy {
		t.Fatalf("expected policy.md among citations, got %+v", ans.Citations)
	}
}

// TestFullPipeline_FileEditTriggersReindex checks that changing a file's
// content (and thus its hash) causes it to be re-chunked/re-embedded, and
// that stale chunks from the old version are removed.
func TestFullPipeline_FileEditTriggersReindex(t *testing.T) {
	srcDir := t.TempDir()
	target := filepath.Join(srcDir, "doc.txt")
	must(t, os.WriteFile(target, []byte("The launch code is ALPHA."), 0o644))

	emb := embedder.NewHashingEmbedder(256)
	vec := index.NewMemoryVectorIndex()
	kw := index.NewBM25KeywordIndex()
	coll := usecase.NewCollection("secrets", srcDir)
	cs := domain.NewCollectionStore(coll)
	ing := &usecase.Ingester{
		Parsers:  []usecase.DocumentParser{parser.TextParser{}},
		Embedder: emb,
		Vector:   vec,
		Keyword:  kw,
		Chunking: usecase.DefaultChunkOptions(),
	}
	must(t, mustIngest(ing, cs, srcDir))

	before := kw.Search("ALPHA", 5)
	if len(before) == 0 {
		t.Fatal("expected to find ALPHA before edit")
	}

	must(t, os.WriteFile(target, []byte("The launch code is BRAVO."), 0o644))
	must(t, mustIngest(ing, cs, srcDir))

	afterOld := kw.Search("ALPHA", 5)
	afterNew := kw.Search("BRAVO", 5)
	if len(afterOld) != 0 {
		t.Fatalf("expected old content ALPHA to be gone after edit, still found %d hits", len(afterOld))
	}
	if len(afterNew) == 0 {
		t.Fatal("expected new content BRAVO to be indexed after edit")
	}
}

func mustIngest(ing *usecase.Ingester, cs *domain.CollectionStore, root string) error {
	_, err := ing.IngestFolder(cs, root)
	return err
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
