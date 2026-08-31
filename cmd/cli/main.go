// Command homebredrag is the headless CLI for HomeBred-RAG: ingest a folder
// of documents into a local collection, then ask grounded questions against
// it. Everything runs on-device — no cloud vector DB, no data leaves the
// machine unless the user opts into a remote generation backend by setting
// ANTHROPIC_API_KEY.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"homebred-rag/internal/adapter/embedder"
	"homebred-rag/internal/adapter/index"
	"homebred-rag/internal/adapter/llm"
	"homebred-rag/internal/adapter/parser"
	"homebred-rag/internal/adapter/watcher"
	"homebred-rag/internal/domain"
	"homebred-rag/internal/infra/storage"
	"homebred-rag/internal/usecase"
)

const embeddingDim = 256

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "index":
		cmdIndex(os.Args[2:])
	case "ask":
		cmdAsk(os.Args[2:])
	case "watch":
		cmdWatch(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`homebredrag — local retrieval-augmented chat over your own files

Usage:
  homebredrag index ./docs --collection tax2025
  homebredrag ask "What was my Q3 deduction?" --collection tax2025
  homebredrag ask "..." --collection tax2025 --export out.md
  homebredrag watch ./docs --collection tax2025
  homebredrag list

Flags are per-subcommand; run "homebredrag <command> -h" for details.
Set ANTHROPIC_API_KEY to enable synthesized (vs. extractive) answers.`)
}

func dataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".homebredrag", "collections")
}

// --- shared wiring -----------------------------------------------------

type app struct {
	store     *storage.FileStore
	embedder  usecase.Embedder
	vector    *index.MemoryVectorIndex
	keyword   *index.BM25KeywordIndex
	parsers   []usecase.DocumentParser
	ocrEngine parser.OCREngine // nil unless built with -tags ocr
}

func newApp() *app {
	ocrEngine, ocrAvailable := parser.NewOCREngine()
	pdfParser := parser.PDFParser{} // OCR fallback disabled unless ocrAvailable
	parsers := []usecase.DocumentParser{
		parser.MarkdownParser{},
		pdfParser,
		parser.NewCodeParser(),
		parser.TextParser{}, // last: catches plain .txt not claimed above
	}
	if ocrAvailable {
		pdfParser = parser.NewPDFParser(ocrEngine)
		parsers[1] = pdfParser
		// Standalone scanned images (receipts, screenshots of printed
		// pages, etc.) alongside the file-type parsers above.
		parsers = append(parsers, parser.ImageParser{OCR: ocrEngine})
	}
	return &app{
		store:     storage.NewFileStore(dataDir()),
		embedder:  embedder.NewHashingEmbedder(embeddingDim),
		vector:    index.NewMemoryVectorIndex(),
		keyword:   index.NewBM25KeywordIndex(),
		parsers:   parsers,
		ocrEngine: ocrEngine,
	}
}

// close releases any OCR engine resources. Safe to call even when OCR
// wasn't compiled in (ocrEngine is nil in that case).
func (a *app) close() {
	if a.ocrEngine != nil {
		a.ocrEngine.Close()
	}
}

// loadOrCreate loads a persisted collection store if present, or creates a
// fresh one, and rebuilds the in-memory search indices from it either way.
func (a *app) loadOrCreate(name, rootPath string) (*domain.CollectionStore, error) {
	coll := usecase.NewCollection(name, rootPath)
	if a.store.Exists(coll.ID) {
		cs, err := a.store.Load(coll.ID)
		if err != nil {
			return nil, err
		}
		usecase.RebuildIndices(cs, a.vector, a.keyword)
		return cs, nil
	}
	return domain.NewCollectionStore(coll), nil
}

func (a *app) llmClient() usecase.LLMClient {
	if remote, ok := llm.NewAnthropicClient(); ok {
		return remote
	}
	return llm.ExtractiveClient{}
}

// --- index ---------------------------------------------------------------

func cmdIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	collection := fs.String("collection", "", "collection name (required)")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 || *collection == "" {
		fmt.Fprintln(os.Stderr, "usage: homebredrag index <folder> --collection <name>")
		os.Exit(1)
	}
	root := fs.Arg(0)

	a := newApp()
	defer a.close()
	cs, err := a.loadOrCreate(*collection, root)
	must(err)

	ing := &usecase.Ingester{
		Parsers:  a.parsers,
		Embedder: a.embedder,
		Vector:   a.vector,
		Keyword:  a.keyword,
		Chunking: usecase.DefaultChunkOptions(),
	}
	res, err := ing.IngestFolder(cs, root)
	must(err)
	must(a.store.Save(cs))

	fmt.Printf("Indexed %q from %s\n", *collection, root)
	fmt.Printf("  added: %d  updated: %d  unchanged: %d  skipped: %d  errors: %d\n",
		len(res.Added), len(res.Updated), len(res.Unchanged), len(res.Skipped), len(res.Errors))
	for path, err := range res.Errors {
		fmt.Printf("  ! %s: %v\n", path, err)
	}
	fmt.Printf("  total chunks in collection: %d\n", len(cs.Chunks))
}

// --- ask -------------------------------------------------------------

func cmdAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	collection := fs.String("collection", "", "collection name (required)")
	topK := fs.Int("topk", 5, "number of chunks to retrieve")
	exportPath := fs.String("export", "", "write the Q&A (with citations) as markdown to this path")
	jsonOut := fs.Bool("json", false, "print the raw Answer as JSON")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 || *collection == "" {
		fmt.Fprintln(os.Stderr, `usage: homebredrag ask "question" --collection <name>`)
		os.Exit(1)
	}
	question := fs.Arg(0)

	a := newApp()
	defer a.close()
	coll := usecase.NewCollection(*collection, "")
	if !a.store.Exists(coll.ID) {
		fmt.Fprintf(os.Stderr, "no such collection %q — run `homebredrag index` first\n", *collection)
		os.Exit(1)
	}
	cs, err := a.store.Load(coll.ID)
	must(err)
	usecase.RebuildIndices(cs, a.vector, a.keyword)

	answerer := &usecase.Answerer{
		Vector:   a.vector,
		Keyword:  a.keyword,
		Embedder: a.embedder,
		LLM:      a.llmClient(),
		Options:  usecase.DefaultAnswerOptions(),
	}
	ans, err := answerer.AnswerQuestion(domain.Query{CollectionID: cs.Collection.ID, Text: question, TopK: *topK})
	must(err)

	if *jsonOut {
		b, _ := json.MarshalIndent(ans, "", "  ")
		fmt.Println(string(b))
		return
	}

	fmt.Println(ans.Text)
	if ans.Answerable && len(ans.Citations) > 0 {
		fmt.Println("\nSources:")
		for _, c := range ans.Citations {
			fmt.Printf("  - %s (score %.3f)\n", c.DocumentPath, c.Score)
		}
	}

	if *exportPath != "" {
		md := usecase.ExportSessionMarkdown(*collection, []usecase.QAExchange{{Question: question, Answer: ans}})
		must(os.WriteFile(*exportPath, []byte(md), 0o644))
		fmt.Printf("\nExported to %s\n", *exportPath)
	}
}

// --- watch -----------------------------------------------------------

func cmdWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	collection := fs.String("collection", "", "collection name (required)")
	interval := fs.Duration("interval", 5*time.Second, "poll interval")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 || *collection == "" {
		fmt.Fprintln(os.Stderr, "usage: homebredrag watch <folder> --collection <name>")
		os.Exit(1)
	}
	root := fs.Arg(0)

	a := newApp()
	defer a.close()
	cs, err := a.loadOrCreate(*collection, root)
	must(err)

	ing := &usecase.Ingester{
		Parsers:  a.parsers,
		Embedder: a.embedder,
		Vector:   a.vector,
		Keyword:  a.keyword,
		Chunking: usecase.DefaultChunkOptions(),
	}

	reindex := func() {
		res, err := ing.IngestFolder(cs, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reindex error: %v\n", err)
			return
		}
		if len(res.Added)+len(res.Updated) > 0 {
			fmt.Printf("[%s] re-indexed: +%d added, %d updated\n",
				time.Now().Format(time.Kitchen), len(res.Added), len(res.Updated))
			must(a.store.Save(cs))
		}
	}
	reindex() // initial full pass
	fmt.Printf("Watching %s for %q (every %s). Ctrl+C to stop.\n", root, *collection, *interval)

	w := watcher.NewPollWatcher(root, *interval, reindex)
	w.Run(context.Background())
}

// --- list --------------------------------------------------------------

func cmdList(args []string) {
	a := newApp()
	defer a.close()
	ids, err := a.store.List()
	must(err)
	if len(ids) == 0 {
		fmt.Println("No collections yet — run `homebredrag index <folder> --collection <name>`.")
		return
	}
	for _, id := range ids {
		cs, err := a.store.Load(id)
		if err != nil {
			continue
		}
		fmt.Printf("%-20s  %5d docs  %6d chunks  root=%s\n",
			cs.Collection.Name, len(cs.Documents), len(cs.Chunks), cs.Collection.RootPath)
	}
}

func splitFlagsAndArgs(args []string) (flags []string, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			// Consume a following value unless it's itself a flag or this is a boolean flag.
			if i+1 < len(args) && !(len(args[i+1]) > 1 && args[i+1][0] == '-') && a != "-json" && a != "--json" {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, a)
		}
	}
	return flags, positional
}

// reorder puts positional args after all flags, then flags again, so users
// can write either `index ./docs --collection x` or `index --collection x ./docs`
// — Go's flag package only supports the latter natively.
func reorderArgs(args []string) []string {
	flags, positional := splitFlagsAndArgs(args)
	return append(flags, positional...)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
