# HomeBred-RAG

A local-first "chat with my files" tool: point it at a folder of Markdown,
text, code, PDF, or scanned-image files, and it ingests, chunks, embeds, and
indexes them entirely on your machine, then answers questions with citations
back to the source file. No cloud vector DB, no data leaves your machine
unless you explicitly opt into a remote LLM for the final answer-generation
step.

Two ways to use it:

- **`cmd/cli`** — a headless CLI, zero external dependencies by default.
- **`cmd/desktop`** — a native Wails desktop app (GTK3/WebKit2GTK on Linux)
  over the same backend.

Both are thin shells over the same `internal/usecase` layer — the clean
architecture split (`domain` / `usecase` / `adapter` / `infra`) from
`04-homebred-rag.md` means the ingest/retrieve/answer logic doesn't know or
care which shell is driving it.

## Build & run — CLI

Requires Go 1.22+. No system dependencies, no `go get` needed for the base
build.

```bash
go build -o bin/homebredrag ./cmd/cli
go test ./...              # unit + integration + retrieval-regression tests
```

```bash
# Index a folder into a named collection (safe to re-run — unchanged
# files are skipped via content hashing; edited files are re-embedded).
./bin/homebredrag index ./docs --collection tax2025

# Ask a question. Answers are extractive (quoted passages + citations) by
# default. Set ANTHROPIC_API_KEY to get a real synthesized, cited answer
# from Claude instead.
./bin/homebredrag ask "What was my Q3 deduction?" --collection tax2025

# Export a Q&A session (with citations) to a markdown note.
./bin/homebredrag ask "..." --collection tax2025 --export session.md

# Get raw JSON (chunk_id, snippet, score per citation).
./bin/homebredrag ask "..." --collection tax2025 --json

# Poll a folder and auto-reindex on changes (Ctrl+C to stop).
./bin/homebredrag watch ./docs --collection tax2025

# List all collections and their size.
./bin/homebredrag list
```

Flags can go before or after the positional folder/question argument —
both `index ./docs --collection x` and `index --collection x ./docs` work.

Collections persist under `~/.homebredrag/collections/<id>.json`, shared
between the CLI and the desktop app (index with one, ask with the other).

## OCR for scanned documents (optional, `-tags ocr`)

The default build has zero external dependencies and simply doesn't
extract text from scanned PDFs or images. Building with `-tags ocr` adds a
real Tesseract-backed OCR fallback:

- **Standalone scanned images** (`.png`, `.jpg`, `.jpeg`, `.tiff`, `.bmp`) —
  OCR'd directly and ingested like any other document.
- **Scanned (image-only) PDFs** — if direct text extraction comes back too
  sparse to be useful (under `PDFParser.MinTextChars`, default 80 chars),
  `PDFParser` rasterizes each page with `pdftoppm` and OCRs the page images.
  If `pdftoppm` isn't on `PATH`, it degrades gracefully to whatever direct
  extraction found rather than erroring the ingest.

Requires Tesseract's dev headers at build time (via
[`gosseract`](https://github.com/otiai10/gosseract), which uses cgo) and
`poppler-utils` for the scanned-PDF rasterization step:

```bash
# Debian/Ubuntu
apt install tesseract-ocr libtesseract-dev libleptonica-dev poppler-utils

go build -tags ocr -o bin/homebredrag ./cmd/cli
go test -tags ocr ./...     # includes real OCR tests against generated
                             # scanned-image/scanned-PDF fixtures
```

The Tesseract runtime (not just the dev headers) needs to be installed
wherever the resulting binary actually *runs*, same as any cgo dependency.

Without `-tags ocr`, `OCREngine` resolves to a stub that reports itself
unavailable (`ocr_engine_stub.go`), `ImageParser` simply doesn't claim any
files, and `PDFParser`'s OCR fallback is a no-op — scanned documents get
skipped/empty rather than erroring, and nothing about the default build
changes.

## Build & run — desktop app

A native Wails v2 app: same collections, same ingest/ask/export flow, a
window instead of a terminal.

```bash
# Debian/Ubuntu (newer distros — Ubuntu 24.04+, Debian 13+ — ship
# webkit2gtk 4.1; use the webkit2_41 build tag for those)
apt install libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config

go build -tags desktop,production,webkit2_41 -o bin/homebredrag-desktop ./cmd/desktop

# Add OCR support too:
go build -tags desktop,production,webkit2_41,ocr -o bin/homebredrag-desktop ./cmd/desktop
```

On older distros that still package `webkit2gtk-4.0`, drop the
`webkit2_41` tag (`-tags desktop,production` is enough) and install
`libwebkit2gtk-4.0-dev` instead.

Fetching the Wails module itself needs one workaround if your Go module
proxy is unreachable: this repo's `go.mod` already includes `replace`
directives redirecting `golang.org/x/*` and `gopkg.in/*` imports to their
GitHub mirrors, since Wails' transitive dependencies pull those in via
vanity import paths that need `golang.org`/`gopkg.in` reachable to resolve
otherwise. With a normal Go module proxy available you don't need to think
about this at all; the replace directives are harmless either way.

The window itself: a sidebar lists collections with doc/chunk counts,
"+ New collection" opens a form with a native folder picker
(`ChooseFolder`, via `runtime.OpenDirectoryDialog`) and an "Index folder"
button; selecting a collection opens a chat-style pane where questions get
answered with the same citations/confidence/backend info the CLI shows,
plus a per-answer "Export .md" button (native save dialog). See
`cmd/desktop/frontend/dist/` for the vanilla HTML/CSS/JS frontend — no
npm/build toolchain involved, Wails serves it straight from the embedded
`embed.FS`.

**If you're extending the frontend:** don't name a top-level JS helper
`go` — see the comment at the top of `app.js`. A global `function go(){}`
declaration assigns to `window.go`, which is exactly where Wails injects
its own bindings (`window.go.main.App`); naming collision silently
clobbers them and every backend call fails with a confusing "undefined"
error. Learned this the hard way building the reference app.

## What's implemented

- **Ingestion** (`internal/usecase/ingest_document.go`): recursive folder
  walk, SHA-256 content hashing so re-running `index` only re-embeds files
  that actually changed, and prunes chunks for files deleted from disk.
- **Chunking** (`chunk_document.go`): fixed-size word windows with
  configurable overlap; offsets stay aligned to the original file bytes so
  citations can point at an exact spot in the source.
- **Parsers** (`adapter/parser/`): Markdown (markup-stripped, offset-safe),
  plain text, ~15 source-code extensions, a minimal pure-Go PDF text
  extractor (inflates `FlateDecode` streams, reads `Tj`/`TJ` operators —
  handles typical exported/printed PDFs), and — with `-tags ocr` — scanned
  images and scanned PDFs via Tesseract (see above).
- **Embedding** (`adapter/embedder/hashing_embedder.go`): local
  feature-hashing (bag-of-words + bigrams → fixed vector, L2-normalized).
  See caveat below.
- **Retrieval** (`adapter/index/`, `usecase/hybrid_search.go`): brute-force
  cosine vector search + a from-scratch BM25 keyword index, fused with
  Reciprocal Rank Fusion — vector search alone misses exact terms (IDs,
  names, code symbols); BM25 catches those.
- **Confidence guardrail** (`usecase/answer_question.go`): if the top
  retrieval score is below a threshold, the app says it can't answer from
  the documents rather than guessing.
- **Answer generation** (`adapter/llm/`): `ExtractiveClient` (default,
  fully offline, assembles retrieved passages) or `RemoteClient` (calls the
  Anthropic API for a real synthesized, cited answer if `ANTHROPIC_API_KEY`
  is set) — both implement the same `LLMClient` interface, so any other
  backend (OpenAI, a local HomeBred-LLM runtime) is a new adapter, not a
  rewrite.
- **Persistence** (`infra/storage/`): each collection's documents/chunks/
  embeddings as a single JSON file, shared between the CLI and desktop app.
- **File watcher** (`adapter/watcher/`): polling-based; re-ingests on an
  interval and relies on the ingester's own hash check to make no-op polls
  cheap.
- **Export** (`usecase/export_session.go`): Q&A sessions with citations to
  a markdown note, from either shell.
- **Desktop shell** (`cmd/desktop/`): Wails v2 app binding the same usecase
  layer, with a native folder picker and save dialog.

Tests: chunking boundary/overlap correctness, parser fixtures, a retrieval
regression suite (fixed doc set + known question→expected-source pairs, per
the plan's testing strategy), a confidence-guardrail test, two integration
tests covering the full ingest → persist → reload → query round trip and
edit-triggers-reindex behavior, and (with `-tags ocr`) real end-to-end OCR
tests against generated scanned-image/scanned-PDF fixtures. `go vet` and
`go test -race` both clean, in both build modes.

## What's different from the original plan, and why

The very first version of this build was done entirely offline (no Go
module proxy, no display) and used dependency-free stand-ins for anything
requiring `go get` or a GUI. OCR and the desktop app were later added for
real once it turned out this environment *could* reach GitHub directly (see
the `replace`-directive workaround above) and had `Xvfb`/GTK/WebKit
available to actually build and exercise a native window. What's left
approximated:

| Plan (section 5)                          | This build                                  | Swap-in path |
|---|---|---|
| `bge-small`/`e5-small` via ONNX runtime   | Feature-hashing embedder                    | New `Embedder` adapter — see caveat below |
| SQLite + `sqlite-vec` (cgo)               | Per-collection JSON file                    | New adapter behind `infra/storage`'s load/save contract |
| `blevesearch/bleve` for keyword search    | Hand-rolled BM25 inverted index             | New adapter behind `usecase.KeywordIndex` |
| `fsnotify`                                | Interval polling                            | New adapter behind the same `OnChange` callback contract |
| `cobra` CLI framework                     | Stdlib `flag`, hand-rolled subcommands      | Cosmetic; behavior is equivalent |

Wails desktop GUI and OCR (`gosseract`/Tesseract) are no longer on this
list — both are implemented for real, described above, and verified
end-to-end: the desktop app was actually run under a virtual display
(`Xvfb`) and driven through a full index → select collection → ask →
answer-with-citations flow to confirm it works, not just that it compiles.

**The one caveat worth flagging directly**: the hashing embedder is a real,
working local embedding scheme (the classic hashing trick), and the
retrieval-quality tests pass — but it's meaningfully weaker than a trained
embedding model like `bge-small`. It captures lexical overlap well (so it's
fine for exact/near-exact term matches, which is also why hybrid search
with BM25 matters here) but won't generalize across paraphrases or synonyms
the way a real sentence embedding model does. Treat it as a functional
placeholder, not a production embedding quality bar — swapping in the ONNX
model from the original plan is the highest-leverage upgrade if you take
this further.

## Project layout

```
cmd/cli/main.go                     # CLI entry point, wires adapters to usecases
cmd/desktop/main.go                 # Wails entry point, embeds frontend/dist
cmd/desktop/app.go                  # Wails-bound backend (same usecase layer)
cmd/desktop/frontend/dist/          # vanilla HTML/CSS/JS UI, no build step
internal/domain/                    # entities: Document, Chunk, Collection, Answer...
internal/usecase/                   # ingestion, chunking, hybrid search, answering — no I/O
internal/adapter/parser/            # markdown, text, code, pdf, ocr (image + scanned-PDF)
internal/adapter/embedder/          # hashing_embedder.go
internal/adapter/index/             # memory_vector_index.go, bm25_keyword_index.go
internal/adapter/llm/               # extractive_client.go, remote_client.go
internal/adapter/watcher/           # poll_watcher.go
internal/infra/storage/             # collection_store.go (JSON persistence)
```
