package usecase

import (
	"fmt"
	"strings"
	"time"

	"homebred-rag/internal/domain"
)

// QAExchange is one question/answer pair in a session export.
type QAExchange struct {
	Question string
	Answer   domain.Answer
}

// ExportSessionMarkdown renders a Q&A session (with citations) as a single
// markdown document suitable for `homebredrag ask --export`.
func ExportSessionMarkdown(collectionName string, exchanges []QAExchange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# HomeBred-RAG session — %s\n\n", collectionName)
	fmt.Fprintf(&b, "_Exported %s_\n\n", time.Now().Format(time.RFC3339))

	for i, ex := range exchanges {
		fmt.Fprintf(&b, "## Q%d: %s\n\n", i+1, ex.Question)
		if !ex.Answer.Answerable {
			b.WriteString(ex.Answer.Text + "\n\n")
			continue
		}
		b.WriteString(ex.Answer.Text + "\n\n")
		if len(ex.Answer.Citations) > 0 {
			b.WriteString("**Sources:**\n\n")
			for _, c := range ex.Answer.Citations {
				fmt.Fprintf(&b, "- `%s` (score %.3f): %s\n", c.DocumentPath, c.Score, strings.ReplaceAll(c.Snippet, "\n", " "))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
