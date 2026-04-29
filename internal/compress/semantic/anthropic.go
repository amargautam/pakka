// Anthropic API client for the semantic Rewriter. Pure stdlib — net/http +
// encoding/json. No vendor SDK. Reads ANTHROPIC_API_KEY from the environment;
// when missing, callers (CLI wiring) should fall back to deterministic mode
// rather than failing the call.
//
// The model is configurable via PAKKA_COMPRESS_MODEL (default
// claude-haiku-4-5-20251001). No model ID is hard-coded into business logic.
package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DefaultModel is the cheap-fast default for prose rewrite. Override with
// PAKKA_COMPRESS_MODEL.
const DefaultModel = "claude-haiku-4-5-20251001"

// DefaultEndpoint is the documented Anthropic Messages endpoint.
const DefaultEndpoint = "https://api.anthropic.com/v1/messages"

// AnthropicClient implements Rewriter and FixRewriter against the Anthropic
// Messages API. It is safe for concurrent use; net/http.Client manages the
// underlying connection pool.
type AnthropicClient struct {
	APIKey   string
	Model    string
	Endpoint string
	HTTP     *http.Client
}

// NewAnthropicClient builds a client from env. Returns nil + ok=false when
// ANTHROPIC_API_KEY is not set — callers fall back to deterministic mode.
//
// Purpose: Lazy-initialize one production rewriter when an API key is present.
// Errors: None; absence of an API key is signaled via the second return.
func NewAnthropicClient() (*AnthropicClient, bool) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, false
	}
	model := os.Getenv("PAKKA_COMPRESS_MODEL")
	if model == "" {
		model = DefaultModel
	}
	return &AnthropicClient{
		APIKey:   key,
		Model:    model,
		Endpoint: DefaultEndpoint,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}, true
}

// messagesRequest mirrors the documented JSON shape for /v1/messages.
type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []messagesTurn   `json:"messages"`
	System    string           `json:"system,omitempty"`
}

type messagesTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesResponse captures the one field we read.
type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	// We ignore usage/stop_reason for now — the validator is the source of
	// truth, and we don't bill on tokens here (the host pays for the key).
}

// Rewrite implements Rewriter by POSTing to /v1/messages with the level
// template body as the user turn.
//
// Purpose: Production rewrite path used by the CLI when ANTHROPIC_API_KEY is
// set.
// Errors: Wraps any HTTP, decode, or non-2xx response error.
func (c *AnthropicClient) Rewrite(ctx context.Context, input string, level Level) (string, error) {
	prompt, err := renderPrompt(level, input)
	if err != nil {
		return "", err
	}
	return c.post(ctx, prompt)
}

// RewriteFix implements FixRewriter — same wire format, different prompt.
func (c *AnthropicClient) RewriteFix(ctx context.Context, input string, level Level, violations []Violation) (string, error) {
	prompt, err := renderFixPrompt(level, input, violations)
	if err != nil {
		return "", err
	}
	return c.post(ctx, prompt)
}

// post issues the HTTP request and decodes the first text block from the
// response. Returns an error on non-2xx or empty-content responses.
func (c *AnthropicClient) post(ctx context.Context, prompt string) (string, error) {
	body := messagesRequest{
		Model:     c.Model,
		MaxTokens: 8192,
		Messages: []messagesTurn{
			{Role: "user", Content: prompt},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("semantic: marshal: %w", err)
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("semantic: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("X-Api-Key", c.APIKey)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("semantic: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("semantic: read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		// Cap the message snippet to avoid leaking large payloads to logs.
		return "", fmt.Errorf("semantic: status %d: %s", resp.StatusCode, snippet(respBody, 200))
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("semantic: decode: %w", err)
	}
	for _, blk := range parsed.Content {
		if blk.Type == "text" && blk.Text != "" {
			return blk.Text, nil
		}
	}
	return "", fmt.Errorf("semantic: empty response")
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
