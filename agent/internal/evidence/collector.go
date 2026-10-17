// internal/evidence/collector.go
// Collects evidence for an incident from Loki and Prometheus.
// Results are redacted before being returned so callers can safely pass
// them to the LLM without leaking secrets.

package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devops-sentinel/agent/internal/redact"
)

// Evidence holds the collected and redacted evidence for one incident.
type Evidence struct {
	// LogExcerpt is the redacted recent error log lines from Loki.
	LogExcerpt string
	// LogLineCount is how many raw lines were returned.
	LogLineCount int
	// MetricsSummary is the formatted metrics snapshot from Prometheus.
	MetricsSummary string
	// CollectedAt is when the evidence was gathered (UTC).
	CollectedAt time.Time
}

// Collector fetches evidence from Loki and Prometheus.
type Collector struct {
	LokiURL       string
	PrometheusURL string
	HTTPClient    *http.Client
}

func (c *Collector) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Collect gathers logs and metrics for the given service and alert.
// Both sources are queried; an error in one does not prevent the other.
// Partial results are returned with an error summary in the affected field.
func (c *Collector) Collect(ctx context.Context, alertName, service string) Evidence {
	ev := Evidence{CollectedAt: time.Now().UTC()}

	// Fetch logs and metrics concurrently using channels.
	type logResult struct {
		excerpt string
		count   int
		err     error
	}
	type metricResult struct {
		summary string
		err     error
	}

	logCh := make(chan logResult, 1)
	metCh := make(chan metricResult, 1)

	go func() {
		excerpt, count, err := c.fetchLogs(ctx, service)
		logCh <- logResult{excerpt, count, err}
	}()

	go func() {
		summary, err := c.fetchMetrics(ctx, alertName)
		metCh <- metricResult{summary, err}
	}()

	lr := <-logCh
	mr := <-metCh

	if lr.err != nil {
		ev.LogExcerpt = fmt.Sprintf("[log collection error: %s]", lr.err.Error())
	} else {
		// Redact before storing — nothing after this point contains raw secrets.
		ev.LogExcerpt = redact.RedactLines(lr.excerpt)
		ev.LogLineCount = lr.count
	}

	if mr.err != nil {
		ev.MetricsSummary = fmt.Sprintf("[metrics collection error: %s]", mr.err.Error())
	} else {
		ev.MetricsSummary = mr.summary
	}

	return ev
}

// ---------------------------------------------------------------------------
// Loki
// ---------------------------------------------------------------------------

func (c *Collector) fetchLogs(ctx context.Context, service string) (string, int, error) {
	end := time.Now()
	start := end.Add(-15 * time.Minute)

	query := fmt.Sprintf(`{namespace="sentinel"} | json | level="error"`)
	if service != "" {
		query = fmt.Sprintf(`{"service.name"=%q, namespace="sentinel"} | json | level="error"`, service)
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Set("limit", "50")
	params.Set("direction", "backward")

	reqURL := c.LokiURL + "/loki/api/v1/query_range?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build loki request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("loki request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", 0, fmt.Errorf("read loki body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("loki %d: %s", resp.StatusCode, body)
	}

	return parseLokiLogs(body)
}

type lokiQueryResponse struct {
	Data struct {
		Result []struct {
			Values [][]string `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func parseLokiLogs(body []byte) (string, int, error) {
	var r lokiQueryResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", 0, fmt.Errorf("parse loki response: %w", err)
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
	if excerpt == "" {
		excerpt = "[no error logs found in the last 15 minutes]"
	}

	return excerpt, len(lines), nil
}

// ---------------------------------------------------------------------------
// Prometheus
// ---------------------------------------------------------------------------

// metricQueries maps alert names to relevant PromQL expressions.
// The label is shown to the LLM; the expr is the PromQL query.
var metricQueries = map[string][]struct{ label, expr string }{
	"BackendDown": {
		{"ready_replicas", `kube_deployment_status_replicas_ready{namespace="sentinel",deployment="backend"}`},
		{"desired_replicas", `kube_deployment_spec_replicas{namespace="sentinel",deployment="backend"}`},
		{"pod_restarts_15m", `sum(increase(kube_pod_container_status_restarts_total{namespace="sentinel"}[15m]))`},
	},
	"HighErrorRate": {
		{"error_rate_per_sec", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app",http_response_status_code=~"5.."}[2m]))`},
		{"total_req_per_sec", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app"}[2m]))`},
		{"pod_restarts_15m", `sum(increase(kube_pod_container_status_restarts_total{namespace="sentinel",pod=~"backend.*"}[15m]))`},
	},
	"HighLatencyP95": {
		{"p95_latency_seconds", `histogram_quantile(0.95,sum(rate(otelcol_http_server_request_duration_seconds_bucket{job="otelcol-app"}[5m]))by(le))`},
		{"p50_latency_seconds", `histogram_quantile(0.50,sum(rate(otelcol_http_server_request_duration_seconds_bucket{job="otelcol-app"}[5m]))by(le))`},
	},
	"PodCrashLooping": {
		{"restarts_15m", `sum(increase(kube_pod_container_status_restarts_total{namespace="sentinel"}[15m]))`},
		{"ready_replicas", `kube_deployment_status_replicas_ready{namespace="sentinel",deployment="backend"}`},
	},
}

// genericQueries are used when the alert name is not in the map above.
var genericQueries = []struct{ label, expr string }{
	{"ready_replicas", `kube_deployment_status_replicas_ready{namespace="sentinel",deployment="backend"}`},
	{"error_rate_per_sec", `sum(rate(otelcol_http_server_request_duration_seconds_count{job="otelcol-app",http_response_status_code=~"5.."}[2m]))`},
	{"p95_latency_seconds", `histogram_quantile(0.95,sum(rate(otelcol_http_server_request_duration_seconds_bucket{job="otelcol-app"}[5m]))by(le))`},
}

func (c *Collector) fetchMetrics(ctx context.Context, alertName string) (string, error) {
	queries, ok := metricQueries[alertName]
	if !ok {
		queries = genericQueries
	}

	var lines []string
	for _, q := range queries {
		val, err := c.instantQuery(ctx, q.expr)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: error (%s)", q.label, err.Error()))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", q.label, val))
	}

	return strings.Join(lines, "\n"), nil
}

func (c *Collector) instantQuery(ctx context.Context, expr string) (string, error) {
	params := url.Values{}
	params.Set("query", expr)
	params.Set("time", fmt.Sprintf("%d", time.Now().Unix()))

	reqURL := c.PrometheusURL + "/api/v1/query?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build prometheus request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("prometheus request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read prometheus body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("prometheus %d: %s", resp.StatusCode, body)
	}

	var r struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse prometheus response: %w", err)
	}
	if r.Status != "success" || len(r.Data.Result) == 0 {
		return "no data", nil
	}
	v := r.Data.Result[0].Value
	if len(v) < 2 {
		return "no data", nil
	}
	s, ok := v[1].(string)
	if !ok {
		return "no data", nil
	}
	return s, nil
}
