// activity/prometheus.go
// FetchMetrics activity — queries Prometheus for a snapshot of key metrics.

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

const PrometheusActivityName = "FetchMetrics"

// PrometheusActivity holds the base URL for the Prometheus HTTP API.
type PrometheusActivity struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (a *PrometheusActivity) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// FetchMetricsInput specifies which alert to gather context for.
type FetchMetricsInput struct {
	// AlertName is used to select relevant PromQL expressions.
	AlertName string
}

// FetchMetricsResult is what the workflow receives.
type FetchMetricsResult struct {
	// Summary is a human-readable snapshot of key metric values.
	Summary string
	// FiringAlerts is the number of currently firing alerts for this name.
	FiringAlerts int
}

// metricsQuery maps an alert name to a set of informative PromQL instant queries.
// Each entry is a label and a PromQL expression.
var metricsQueries = map[string][]struct{ label, expr string }{
	"BackendDown": {
		{"ready replicas", `kube_deployment_status_replicas_ready{namespace="sentinel",deployment="backend"}`},
		{"desired replicas", `kube_deployment_spec_replicas{namespace="sentinel",deployment="backend"}`},
	},
	"HighErrorRate": {
		{"error rate (5xx/s)", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app",http_response_status_code=~"5.."}[2m]))`},
		{"total req/s", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app"}[2m]))`},
	},
	"HighLatencyP95": {
		{"p95 latency (s)", `histogram_quantile(0.95,sum(rate(otelcol_http_server_request_duration_seconds_bucket{job="otelcol-app"}[5m]))by(le))`},
	},
	"PodCrashLooping": {
		{"restart count (15m)", `sum(increase(kube_pod_container_status_restarts_total{namespace="sentinel"}[15m]))`},
	},
}

// FetchMetrics runs a set of instant queries against Prometheus and formats the results.
func (a *PrometheusActivity) FetchMetrics(ctx context.Context, input FetchMetricsInput) (FetchMetricsResult, error) {
	queries, ok := metricsQueries[input.AlertName]
	if !ok {
		// Fall back to a generic query that works for any alert.
		queries = []struct{ label, expr string }{
			{"backend ready replicas", `kube_deployment_status_replicas_ready{namespace="sentinel",deployment="backend"}`},
			{"error rate (5xx/s)", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app",http_response_status_code=~"5.."}[2m]))`},
		}
	}

	var lines []string

	for _, q := range queries {
		val, err := a.instantQuery(ctx, q.expr)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: error (%s)", q.label, err.Error()))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", q.label, val))
	}

	// Also fetch the number of currently firing alerts with this name.
	firingExpr := fmt.Sprintf(`ALERTS{alertname=%q,alertstate="firing"}`, input.AlertName)
	firingVal, err := a.instantQuery(ctx, firingExpr)
	firingCount := 0
	if err == nil && firingVal != "" && firingVal != "0" {
		firingCount = 1
	}

	return FetchMetricsResult{
		Summary:      strings.Join(lines, "\n"),
		FiringAlerts: firingCount,
	}, nil
}

// instantQuery runs a single PromQL instant query and returns the first scalar value.
func (a *PrometheusActivity) instantQuery(ctx context.Context, expr string) (string, error) {
	params := url.Values{}
	params.Set("query", expr)
	params.Set("time", fmt.Sprintf("%d", time.Now().Unix()))

	reqURL := fmt.Sprintf("%s/api/v1/query?%s", a.BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build prometheus request: %w", err)
	}

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("prometheus request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read prometheus response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("prometheus responded %d: %s", resp.StatusCode, body)
	}

	return parsePrometheusInstant(body)
}

// promResponse is a minimal representation of the Prometheus instant query response.
type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []interface{} `json:"value"` // [timestamp float64, value string]
		} `json:"result"`
	} `json:"data"`
}

func parsePrometheusInstant(body []byte) (string, error) {
	var r promResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse prometheus response: %w", err)
	}

	if r.Status != "success" {
		return "", fmt.Errorf("prometheus status: %s", r.Status)
	}

	if len(r.Data.Result) == 0 {
		return "no data", nil
	}

	v := r.Data.Result[0].Value
	if len(v) < 2 {
		return "no data", nil
	}

	val, ok := v[1].(string)
	if !ok {
		return "no data", nil
	}

	return val, nil
}
