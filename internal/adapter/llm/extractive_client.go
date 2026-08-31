package llm

import (
	"fmt"
	"strings"

	"homebred-rag/internal/domain"
)

// ExtractiveClient is a fully local, fully offline "LLM": it doesn't
// generate free text, it just assembles the most relevant retrieved
// chunks into a readable answer. This is the default backend so the app
// works with zero network calls end to end, matching the plan's "fully
// offline mode indicator" requirement (section 9). Swap in RemoteClient
// (or a client that talks to HomeBred-LLM's local runtime) for real
// generation without changing any usecase code.
type ExtractiveClient struct{}

func (ExtractiveClient) Name() string { return "extractive-local" }

func (ExtractiveClient) Answer(question string, context []domain.Chunk) (string, error) {
	if len(context) == 0 {
		return "No relevant context was found.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Based on %d matching passage(s) in your documents:\n\n", len(context))
	for i, c := range context {
		fmt.Fprintf(&b, "%d. (%s) %s\n\n", i+1, c.DocumentPath, truncate(c.Text, 400))
	}
	b.WriteString("(Local extractive mode — set ANTHROPIC_API_KEY to enable generated, synthesized answers instead of raw passages.)")
	return b.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
