package usecase_test

import (
	"testing"

	"homebred-rag/internal/adapter/embedder"
	"homebred-rag/internal/adapter/index"
	"homebred-rag/internal/domain"
	"homebred-rag/internal/usecase"
)

// This is the regression suite called for in the plan (section 12): a small
// fixed document set plus known question/answer pairs, asserting the
// correct chunk is retrieved in top-k. It exists to catch silent retrieval
// regressions when the embedder, chunker, or fusion logic changes — not to
// benchmark embedding quality in an absolute sense.
func fixtureCollection(t *testing.T) (*index.MemoryVectorIndex, *index.BM25KeywordIndex, usecase.Embedder) {
	t.Helper()
	emb := embedder.NewHashingEmbedder(256)
	vec := index.NewMemoryVectorIndex()
	kw := index.NewBM25KeywordIndex()

	docs := map[string]string{
		"onboarding.md": "New engineers should set up their laptop by installing Go 1.23 and running " +
			"the bootstrap script in tools/bootstrap.sh. Ask in #eng-onboarding for help.",
		"expenses.md": "Travel expense reports are due within 30 days of the trip. Submit receipts " +
			"through the Expensify portal linked from the finance wiki page.",
		"deploy.md": "Production deploys happen via the deploy-prod GitHub Action, which requires " +
			"approval from a member of the platform team before it runs.",
		"security.md": "Report any suspected security incident immediately to security@company.example " +
			"and do not discuss details outside the incident channel.",
	}

	for path, text := range docs {
		doc := domain.Document{ID: path, Path: path}
		chunks := usecase.ChunkText(doc, text, usecase.ChunkOptions{SizeWords: 100, OverlapWords: 0})
		chunks = usecase.EmbedChunks(emb, chunks)
		for _, c := range chunks {
			vec.Add(c)
			kw.Add(c)
		}
	}
	return vec, kw, emb
}

func TestRetrieval_KnownQuestionAnswerPairs(t *testing.T) {
	vec, kw, emb := fixtureCollection(t)

	cases := []struct {
		question   string
		expectPath string
	}{
		{"How do I set up my laptop as a new engineer?", "onboarding.md"},
		{"When are travel expense reports due?", "expenses.md"},
		{"Who has to approve a production deploy?", "deploy.md"},
		{"Who do I tell about a security incident?", "security.md"},
	}

	for _, tc := range cases {
		t.Run(tc.question, func(t *testing.T) {
			results := usecase.HybridSearch(vec, kw, emb, tc.question, 3)
			if len(results) == 0 {
				t.Fatalf("no results for %q", tc.question)
			}
			found := false
			for _, r := range results {
				if r.Chunk.DocumentPath == tc.expectPath {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in top-%d for %q, got top result %q",
					tc.expectPath, len(results), tc.question, results[0].Chunk.DocumentPath)
			}
		})
	}
}

func TestRetrieval_KeywordExactTermBoost(t *testing.T) {
	// A literal identifier ("Expensify") should surface the right doc via
	// keyword search even if the embedding alone is fuzzy about it.
	vec, kw, emb := fixtureCollection(t)
	results := usecase.HybridSearch(vec, kw, emb, "Expensify", 2)
	if len(results) == 0 || results[0].Chunk.DocumentPath != "expenses.md" {
		t.Fatalf("expected expenses.md top result for exact-term query, got %+v", results)
	}
}

func TestAnswerQuestion_ConfidenceGuardrail(t *testing.T) {
	vec, kw, emb := fixtureCollection(t)
	answerer := &usecase.Answerer{
		Vector:   vec,
		Keyword:  kw,
		Embedder: emb,
		LLM:      stubLLM{},
		Options:  usecase.DefaultAnswerOptions(),
	}

	// Totally unrelated question should trip the "can't answer" guardrail
	// rather than returning a confident-sounding hallucination.
	ans, err := answerer.AnswerQuestion(domain.Query{Text: "zzzqqxx nonsense unrelated gibberish", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Answerable {
		t.Errorf("expected guardrail to trigger for unrelated query, got answerable=true, confidence=%.3f", ans.Confidence)
	}

	// A well-matched question should answer normally.
	ans2, err := answerer.AnswerQuestion(domain.Query{Text: "When are travel expense reports due?", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !ans2.Answerable {
		t.Errorf("expected answerable=true for well-matched query, confidence=%.3f", ans2.Confidence)
	}
}

type stubLLM struct{}

func (stubLLM) Name() string { return "stub" }
func (stubLLM) Answer(question string, context []domain.Chunk) (string, error) {
	return "stub answer", nil
}
