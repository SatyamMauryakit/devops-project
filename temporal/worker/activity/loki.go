// activity/loki.go
// FetchLogs activity — queries Loki for recent error log lines.
//
// Activities are the ONLY place where I/O is allowed. This file makes HTTP
// requests to Loki. It must not be called directly from workflow code.

package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const LokiActivityName = "FetchLogs"

// LokiActivity holds the configuration for the Loki activity.
// Dependencies are injected at worker startup.
type LokiActivity struct {
	BaseURL    string
	HTTPClient *http.Client
}

// httpClient returns the configured HTTP client or the default one.
func (a *LokiActivity) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// FetchLogsInput parameterises the query.
type FetchLogsInput struct {
	// Service is the value of the service.name label, e.g. "devops-sentinel-backend".
	Service string
	// Duration is how far back to look, e.g. "15m", "1h".
	Duration string
	// Level filters by the level label, e.g. "error". Empty means all levels.
	Level string
}

// FetchLogsResult is what the workflow receives.
type FetchLogsResult struct {
	// LineCount is the number of log lines returned.
	LineCount int
	// Excerpt is the most recent lines joined by newlines, truncated to 2000 chars.
	Excerpt string
}

// FetchLogs queries the Loki HTTP API for log lines matching the input.
// The method name "FetchLogs" is used as the activity name when registered
// with w.RegisterActivity(lokiAct) in main.go.
func (a *LokiActivity) FetchLogs(ctx context.Context, input FetchLogsInput) (FetchLogsResult, error) {
	// Build the LogQL query.
	query := fmt.Sprintf(`{namespace="sentinel", app="backend"}`)
	if input.Service != "" {
		query = fmt.Sprintf(`{"service.name"=%q, namespace="sentinel"}`, input.Service)
	}
	if input.Level != "" {
		query += fmt.Sprintf(` | json | level=%q`, input.Level)
	}

	end := time.Now()
	start := end.Add(-parseDuration(input.Duration))

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Set("limit", "50")
	params.Set("direction", "backward")

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", a.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return FetchLogsResult{}, fmt.Errorf("build loki request: %w", err)
	}

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return FetchLogsResult{}, fmt.Errorf("loki request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return FetchLogsResult{}, fmt.Errorf("read loki response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return FetchLogsResult{}, fmt.Errorf("loki responded %d: %s", resp.StatusCode, body)
	}

	return parseLokiResponse(body)
}

// lokiResponse is a minimal representation of the Loki query_range response.
type lokiResponse struct {
	Data struct {
		Result []struct {
			Values [][]string `json:"values"` // each value is [timestamp_ns, log_line]
		} `json:"result"`
	} `json:"data"`
}

func parseLokiResponse(body []byte) (FetchLogsResult, error) {
	var r lokiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return FetchLogsResult{}, fmt.Errorf("parse loki response: %w", err)
	}

	var lines []string
	for _, stream := range r.Data.Result {
		for _, v := range stream.Values {
			if len(v) >= 2 {
				lines = append(lines, v[1])
			}
		}
	}

	excerpt := strings.Join(lines, "\n")
	if len(excerpt) > 2000 {
		excerpt = excerpt[:2000] + "\n...[truncated]"
	}

	return FetchLogsResult{
		LineCount: len(lines),
		Excerpt:   excerpt,
	}, nil
}

// parseDuration parses strings like "15m", "1h" into time.Duration.
// Falls back to 15 minutes on parse failure.
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 15 * time.Minute
	}
	return d
}
