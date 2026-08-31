package usecase

import "homebred-rag/internal/domain"

// AnswerOptions configures retrieval + the "I don't know" guardrail.
type AnswerOptions struct {
	TopK                int
	ConfidenceThreshold float64 // minimum top cosine similarity to attempt an answer
}

func DefaultAnswerOptions() AnswerOptions {
	return AnswerOptions{TopK: 5, ConfidenceThreshold: 0.15}
}

// Answerer ties retrieval (hybrid search) to generation (an LLMClient),
// with a confidence guardrail so the app admits it can't answer rather
// than letting the LLM hallucinate from weak context.
type Answerer struct {
	Vector   VectorIndex
	Keyword  KeywordIndex
	Embedder Embedder
	LLM      LLMClient
	Options  AnswerOptions
}

func (a *Answerer) AnswerQuestion(q domain.Query) (domain.Answer, error) {
	opts := a.Options
	if opts.TopK == 0 {
		opts = DefaultAnswerOptions()
	}
	topK := q.TopK
	if topK == 0 {
		topK = opts.TopK
	}

	// Confidence is measured on raw vector cosine similarity, since RRF's
	// fused score is a rank-based number with no fixed scale to threshold on.
	pureVector := a.Vector.Search(a.Embedder.Embed(q.Text), 1)
	topScore := 0.0
	if len(pureVector) > 0 {
		topScore = pureVector[0].Score
	}

	if topScore < opts.ConfidenceThreshold {
		return domain.Answer{
			Text:       "I can't find anything in the indexed documents that answers this confidently. Try rephrasing, or check the question is covered by what's in this collection.",
			Confidence: topScore,
			Answerable: false,
		}, nil
	}

	results := HybridSearch(a.Vector, a.Keyword, a.Embedder, q.Text, topK)
	chunks := make([]domain.Chunk, len(results))
	citations := make([]domain.Citation, len(results))
	for i, r := range results {
		chunks[i] = r.Chunk
		citations[i] = domain.Citation{
			ChunkID:      r.Chunk.ID,
			DocumentPath: r.Chunk.DocumentPath,
			Snippet:      snippet(r.Chunk.Text, 220),
			Score:        r.Score,
		}
	}

	text, err := a.LLM.Answer(q.Text, chunks)
	if err != nil {
		return domain.Answer{}, err
	}

	return domain.Answer{
		Text:       text,
		Citations:  citations,
		Confidence: topScore,
		Answerable: true,
	}, nil
}

func snippet(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "…"
}
