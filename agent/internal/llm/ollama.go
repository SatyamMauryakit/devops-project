// internal/llm/ollama.go
// Ollama provider — calls a locally running Ollama instance.
//
// Trade-offs vs Groq:
//   Ollama pros:  Fully local. No data leaves your machine. Free forever.
//                 Good for regulated environments or air-gapped clusters.
//   Ollama cons:  Requires a machine with at least 4 GB RAM for llama3.1:8b.
//                 First inference is slow (~5-30s depending on hardware).
//                 Model quality depends on the model you pull.
//
// Setup:
//   docker run -d -p 11434:11434 --name ollama ollama/ollama:0.3.6
//   docker exec ollama ollama pull llama3.1:8b
//
// API reference: https://github.com/ollama/ollama/blob/main/docs/api.md

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

// OllamaProvider implements Provider using a local Ollama server.
type OllamaProvider struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func (p *OllamaProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 120 * time.Second} // LLM inference can be slow
}

// ollamaGenerateRequest is the Ollama /api/generate request body.
type ollamaGenerateRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Stream  bool   `json:"stream"`  // false = wait for full response
	Format  string `json:"format"`  // "json" asks Ollama to constrain output
	Options struct {
		Temperature float64 `json:"temperature"`
	} `json:"options"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Diagnose calls the Ollama /api/generate endpoint and returns a validated Diagnosis.
// On validation failure it retries once, then returns Fallback().
func (p *OllamaProvider) Diagnose(ctx context.Context, req DiagnoseRequest) (Diagnosis, error) {
	prompt := BuildPrompt(req)

	raw, err := p.generate(ctx, prompt)
	if err != nil {
		slog.Error("ollama generate failed", "error", err.Error())
		return Fallback("ollama request failed: " + err.Error()), nil
	}

	d, err := ParseAndValidate(raw)
	if err != nil {
		slog.Warn("ollama response invalid, retrying once", "error", err.Error())
		// Retry with an explicit correction instruction appended.
		raw2, err2 := p.generate(ctx, prompt+"\n\nYour previous response was invalid. Return ONLY a JSON object matching the schema above.")
		if err2 != nil {
			return Fallback("ollama retry failed: " + err2.Error()), nil
		}
		d, err = ParseAndValidate(raw2)
		if err != nil {
			slog.Error("ollama response invalid after retry", "error", err.Error())
			return Fallback("LLM returned invalid JSON after retry: " + err.Error()), nil
		}
	}

	return d, nil
}

func (p *OllamaProvider) generate(ctx context.Context, prompt string) (string, error) {
	body := ollamaGenerateRequest{
		Model:  p.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}
	body.Options.Temperature = 0.1 // Low temperature for deterministic structured output.

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal ollama request: %w", err)
	}

	url := p.BaseURL + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", fmt.Errorf("read ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama responded %d: %s", resp.StatusCode, respBody)
	}

	var r ollamaGenerateResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return "", fmt.Errorf("parse ollama response: %w", err)
	}

	return r.Response, nil
}
