package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"homebred-rag/internal/domain"
)

// RemoteClient calls a remote model API to synthesize a grounded answer
// from retrieved chunks, instead of just concatenating them. This is the
// plan's "model-agnostic backend" (section 4): retrieval stays fully
// local either way, only the final generation step optionally leaves the
// machine, and only when the user opts in by setting an API key.
type RemoteClient struct {
	APIKey     string
	Model      string
	Endpoint   string
	HTTPClient *http.Client
}

// NewAnthropicClient builds a RemoteClient targeting the Anthropic API.
// Returns (nil, false) if ANTHROPIC_API_KEY isn't set, so callers can
// cleanly fall back to ExtractiveClient.
func NewAnthropicClient() (*RemoteClient, bool) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, false
	}
	return &RemoteClient{
		APIKey:     key,
		Model:      envOr("HOMEBREDRAG_MODEL", "claude-sonnet-4-6"),
		Endpoint:   "https://api.anthropic.com/v1/messages",
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}, true
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (r *RemoteClient) Name() string { return "remote:" + r.Model }

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r *RemoteClient) Answer(question string, context []domain.Chunk) (string, error) {
	if len(context) == 0 {
		return "No relevant context was found.", nil
	}

	var ctx strings.Builder
	for i, c := range context {
		fmt.Fprintf(&ctx, "[%d] Source: %s\n%s\n\n", i+1, c.DocumentPath, c.Text)
	}

	system := "You are a retrieval-augmented assistant. Answer the user's question using ONLY the numbered " +
		"source passages provided. Cite sources inline like [1], [2]. If the passages don't contain the " +
		"answer, say so plainly rather than guessing."

	reqBody := anthropicRequest{
		Model:     r.Model,
		MaxTokens: 1000,
		System:    system,
		Messages: []anthropicMessage{
			{Role: "user", Content: fmt.Sprintf("Sources:\n\n%s\nQuestion: %s", ctx.String(), question)},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", r.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", r.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := r.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("remote LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("unexpected response from remote LLM: %s", string(body))
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("remote LLM error: %s", parsed.Error.Message)
	}
	var out strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String(), nil
}
