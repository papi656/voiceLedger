package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Extraction holds structured data parsed from a transcription by the LLM.
type Extraction struct {
	Price    string `json:"price"`
	Place    string `json:"place"`
	Category string `json:"category"`
	Date     string `json:"date"`
}

// Client sends text to a local LLM for structured extraction.
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates an LLM client pointed at the given host and port.
func NewClient(host, port string, timeout time.Duration) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		client:  &http.Client{Timeout: timeout},
	}
}

// Extract sends transcription text to the LLM and returns structured extraction
// with price, place, category, and the current date.
func (c *Client) Extract(ctx context.Context, text string) (*Extraction, error) {
	reqBody := map[string]any{
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": prompt,
			},
			{
				"role":    "user",
				"content": text,
			},
		},
		"temperature": 0,
		"max_tokens":  200,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading llm response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return parseResponse(respBody)
}

// parseResponse extracts the assistant message content from the OpenAI-compatible
// chat completion JSON and unmarshals it into an Extraction struct.
func parseResponse(body []byte) (*Extraction, error) {
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("parsing llm chat response: %w", err)
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("llm api error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}

	content := cr.Choices[0].Message.Content

	var e Extraction
	// Strip markdown fences if present
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if err := json.Unmarshal([]byte(content), &e); err != nil {
		return nil, fmt.Errorf("parsing llm extraction JSON: %w (raw: %q)", err, cr.Choices[0].Message.Content)
	}

	e.Date = time.Now().Format("2006-01-02")

	return &e, nil
}

// prompt instructs the LLM to extract structured data from user text.
const prompt = `You are a structured data extractor. Given user text (usually a transcription of speech), extract the following fields:

- price: the price, cost, or amount mentioned. Return empty string if not found.
- place: the location, store, venue, or place mentioned. Return empty string if not found.
- category: a short label describing the type of transaction or topic (e.g. "groceries", "dining", "transport", "shopping", "entertainment"). Return empty string if not clear.

Return ONLY a valid JSON object with exactly these three keys: price, place, category.
No additional text, no markdown, no explanation.`
