package index

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"homebred-rag/internal/domain"
	"homebred-rag/internal/usecase"
)

// BM25KeywordIndex is a small dependency-free inverted-index + BM25 scorer.
// It stands in for `blevesearch/bleve` (section 5's chosen keyword engine),
// which needs network access to fetch during `go get` that this build
// environment doesn't have; the usecase.KeywordIndex interface makes bleve
// a drop-in swap later without touching any retrieval logic. What it buys
// over pure vector search: exact recall on names, IDs, and code symbols
// that a hashed embedding can blur together (see plan section 4).
type BM25KeywordIndex struct {
	mu         sync.RWMutex
	chunks     map[string]domain.Chunk
	docTokens  map[string][]string
	postings   map[string]map[string]int // term -> chunkID -> term frequency
	docLen     map[string]int
	totalDocs  int
	totalLen   int
}

func NewBM25KeywordIndex() *BM25KeywordIndex {
	return &BM25KeywordIndex{
		chunks:    map[string]domain.Chunk{},
		docTokens: map[string][]string{},
		postings:  map[string]map[string]int{},
		docLen:    map[string]int{},
	}
}

var kwTokenRe = regexp.MustCompile(`[A-Za-z0-9_]+`)

func tokenize(text string) []string {
	return kwTokenRe.FindAllString(strings.ToLower(text), -1)
}

func (idx *BM25KeywordIndex) Add(c domain.Chunk) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tokens := tokenize(c.Text)
	idx.chunks[c.ID] = c
	idx.docTokens[c.ID] = tokens
	idx.docLen[c.ID] = len(tokens)
	idx.totalDocs++
	idx.totalLen += len(tokens)

	tf := map[string]int{}
	for _, t := range tokens {
		tf[t]++
	}
	for term, freq := range tf {
		if idx.postings[term] == nil {
			idx.postings[term] = map[string]int{}
		}
		idx.postings[term][c.ID] = freq
	}
}

func (idx *BM25KeywordIndex) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tokens, ok := idx.docTokens[id]
	if !ok {
		return
	}
	idx.totalDocs--
	idx.totalLen -= len(tokens)
	delete(idx.docTokens, id)
	delete(idx.docLen, id)
	delete(idx.chunks, id)

	seen := map[string]bool{}
	for _, t := range tokens {
		if seen[t] {
			continue
		}
		seen[t] = true
		if p, ok := idx.postings[t]; ok {
			delete(p, id)
			if len(p) == 0 {
				delete(idx.postings, t)
			}
		}
	}
}

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

func (idx *BM25KeywordIndex) Search(query string, topK int) []usecase.ScoredChunk {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil
	}
	avgDocLen := float64(idx.totalLen) / float64(idx.totalDocs)

	queryTerms := uniqueTokens(tokenize(query))
	scores := map[string]float64{}

	for _, term := range queryTerms {
		posting, ok := idx.postings[term]
		if !ok {
			continue
		}
		df := len(posting)
		idf := math.Log(1 + (float64(idx.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
		for chunkID, tf := range posting {
			dl := float64(idx.docLen[chunkID])
			denom := float64(tf) + bm25K1*(1-bm25B+bm25B*dl/avgDocLen)
			scores[chunkID] += idf * (float64(tf) * (bm25K1 + 1)) / denom
		}
	}

	out := make([]usecase.ScoredChunk, 0, len(scores))
	for id, score := range scores {
		out = append(out, usecase.ScoredChunk{Chunk: idx.chunks[id], Score: score})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

func uniqueTokens(tokens []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tokens {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
