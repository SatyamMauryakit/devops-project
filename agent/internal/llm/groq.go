// internal/llm/groq.go
// Groq provider — calls the Groq Cloud API (free tier available).
//
// Trade-offs vs Ollama:
//   Groq pros:  Extremely fast inference (< 1 second for 8B models).
//               No local GPU/RAM requirements. Zero infrastructure to manage.
//               Free tier: 6,000 requests/day, 500,000 tokens/day on llama3-8b-8192.
//   Groq cons:  Data leaves your network (sent to Groq's servers).
//               Requires an API key (stored in a Kubernetes Secret).
//               Free tier has rate limits; paid tier needed for production volume.
//               Service availability depends on Groq's uptime.
//
// Free tier signup: https://console.groq.com
// Supported models: llama3-8b-8192, llama3-70b-8192, mixtral-8x7b-32768
//
// API reference: https://console.groq.com/docs/openai (OpenAI-compatible)

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const groqAPIURL = "https://api.groq.com/openai/v1/chat/completions"

// GroqProvider implements Provider using the Groq Cloud API.
type GroqProvider struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func (p *GroqProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// OpenAI-compatible request/response types.
type groqRequest struct {
	Model          string        `json:"model"`
	Messages       []groqMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	ResponseFormat *groqFormat   `json:"response_format,omitempty"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqFormat struct {
	Type string `json:"type"` // "json_object" enforces JSON output
}

type groqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Diagnose calls the Groq API and returns a validated Diagnosis.
// On validation failure it retries once, then returns Fallback().
func (p *GroqProvider) Diagnose(ctx context.Context, req DiagnoseRequest) (Diagnosis, error) {
	prompt := BuildPrompt(req)

	raw, err := p.complete(ctx, prompt)
	if err != nil {
		slog.Error("groq complete failed", "error", err.Error())
		return Fallback("groq request failed: " + err.Error()), nil
	}

	d, err := ParseAndValidate(raw)
	if err != nil {
		slog.Warn("groq response invalid, retrying once", "error", err.Error())
		raw2, err2 := p.complete(ctx, prompt+"\n\nYour previous response was invalid. Return ONLY a JSON object matching the schema above. No markdown, no explanation.")
		if err2 != nil {
			return Fallback("groq retry failed: " + err2.Error()), nil
		}
		d, err = ParseAndValidate(raw2)
		if err != nil {
			slog.Error("groq response invalid after retry", "error", err.Error())
			return Fallback("LLM returned invalid JSON after retry: " + err.Error()), nil
		}
	}

	return d, nil
}

func (p *GroqProvider) complete(ctx context.Context, prompt string) (string, error) {
	body := groqRequest{
		Model: p.Model,
		Messages: []groqMessage{
			{
				Role: "system",
				Content: "You are a senior site reliability engineer. " +
					"You respond ONLY with valid JSON matching the schema provided. " +
					"No markdown, no preamble, no explanation outside the JSON object.",
			},
			{Role: "user", Content: prompt},
		},
		Temperature:    0.1,
		MaxTokens:      1024,
		ResponseFormat: &groqFormat{Type: "json_object"},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal groq request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAPIURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build groq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// API key is passed as a Bearer token — never logged or sent to the LLM.
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("groq request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", fmt.Errorf("read groq response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq responded %d: %s", resp.StatusCode, respBody)
	}

	var r groqResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return "", fmt.Errorf("parse groq response: %w", err)
	}

	if r.Error != nil {
		return "", fmt.Errorf("groq API error (%s): %s", r.Error.Type, r.Error.Message)
	}

	if len(r.Choices) == 0 {
		return "", fmt.Errorf("groq returned no choices")
	}

	return r.Choices[0].Message.Content, nil
}
