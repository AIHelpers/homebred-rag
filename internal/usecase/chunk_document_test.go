package usecase

import (
	"strconv"
	"strings"
	"testing"

	"homebred-rag/internal/domain"
)

func words(n int) string {
	ws := make([]string, n)
	for i := range ws {
		ws[i] = "w" + strconv.Itoa(i)
	}
	return strings.Join(ws, " ")
}

func TestChunkText_EmptyInput(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	chunks := ChunkText(doc, "", DefaultChunkOptions())
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for empty text, got %d", len(chunks))
	}
}

func TestChunkText_SingleChunkWhenShort(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	text := words(50)
	chunks := ChunkText(doc, text, ChunkOptions{SizeWords: 220, OverlapWords: 40})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != text {
		t.Fatalf("expected full text preserved, got %q", chunks[0].Text)
	}
}

func TestChunkText_OverlapCorrectness(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	text := words(100)
	opts := ChunkOptions{SizeWords: 30, OverlapWords: 10}
	chunks := ChunkText(doc, text, opts)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Consecutive chunks should share exactly OverlapWords words at the boundary.
	for i := 0; i < len(chunks)-1; i++ {
		a := strings.Fields(chunks[i].Text)
		b := strings.Fields(chunks[i+1].Text)
		tailA := a[len(a)-opts.OverlapWords:]
		headB := b[:opts.OverlapWords]
		for j := range tailA {
			if tailA[j] != headB[j] {
				t.Fatalf("chunk %d/%d overlap mismatch at %d: %q vs %q", i, i+1, j, tailA[j], headB[j])
			}
		}
	}
}

func TestChunkText_OffsetsRoundTrip(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	text := "The quick brown fox jumps over the lazy dog. " + words(300)
	chunks := ChunkText(doc, text, DefaultChunkOptions())
	for _, c := range chunks {
		got := strings.TrimSpace(text[c.StartOffset:c.EndOffset])
		if got != c.Text {
			t.Fatalf("offset mismatch: chunk text %q != text[%d:%d] %q", c.Text, c.StartOffset, c.EndOffset, got)
		}
	}
}

func TestChunkText_LastChunkNoTinyOrphan(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	// 205 words with size=100/overlap=20 (step 80): windows start at 0,80,160 -> last window
	// covers [160:205], 45 words, not a 1-word orphan. Just assert full coverage, no crash.
	text := words(205)
	chunks := ChunkText(doc, text, ChunkOptions{SizeWords: 100, OverlapWords: 20})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	last := chunks[len(chunks)-1]
	if !strings.HasSuffix(strings.TrimSpace(text), lastWord(last.Text)) {
		t.Fatalf("last chunk should reach end of text, got tail %q", lastWord(last.Text))
	}
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func TestChunkText_IDsAreStableAndUnique(t *testing.T) {
	doc := domain.Document{ID: "d1"}
	text := words(300)
	chunks := ChunkText(doc, text, DefaultChunkOptions())
	seen := map[string]bool{}
	for _, c := range chunks {
		if seen[c.ID] {
			t.Fatalf("duplicate chunk ID %s", c.ID)
		}
		seen[c.ID] = true
	}
	// Re-chunking identical input should reproduce identical IDs.
	again := ChunkText(doc, text, DefaultChunkOptions())
	for i := range chunks {
		if chunks[i].ID != again[i].ID {
			t.Fatalf("chunk IDs not stable across re-chunk: %s != %s", chunks[i].ID, again[i].ID)
		}
	}
}
