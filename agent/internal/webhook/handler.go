// internal/webhook/handler.go
// HTTP handler for POST /webhook — receives Alertmanager payloads,
// collects evidence, runs LLM diagnosis, and starts a Temporal workflow.

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/sdk/client"

	"github.com/devops-sentinel/agent/internal/evidence"
	"github.com/devops-sentinel/agent/internal/llm"
)

const tracerName = "devops-sentinel/agent/webhook"

// ---------------------------------------------------------------------------
// Alertmanager payload types (same shape as in the notifier from Step 6)
// ---------------------------------------------------------------------------

type alertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []alertItem       `json:"alerts"`
}

type alertItem struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
}

// ---------------------------------------------------------------------------
// IncidentInput mirrors the type in the Temporal worker (Step 7).
// Duplicated here to avoid a cross-module import; kept in sync manually.
// ---------------------------------------------------------------------------

type incidentInput struct {
	AlertName   string `json:"AlertName"`
	Severity    string `json:"Severity"`
	Summary     string `json:"Summary"`
	Description string `json:"Description"`
	StartsAt    string `json:"StartsAt"`
	// Diagnosis is injected by this agent before starting the workflow.
	DiagnosisSummary   string   `json:"DiagnosisSummary"`
	RootCause          string   `json:"RootCause"`
	Confidence         string   `json:"Confidence"`
	ProposedAction     string   `json:"ProposedAction"`
	ActionRationale    string   `json:"ActionRationale"`
	EvidenceUsed       []string `json:"EvidenceUsed"`
}

// ---------------------------------------------------------------------------
// Handler dependencies
// ---------------------------------------------------------------------------

// Handler processes incoming Alertmanager webhook calls.
type Handler struct {
	Collector      *evidence.Collector
	LLM            llm.Provider
	TemporalClient client.Client
	TaskQueue      string
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "webhook.handle")
	defer span.End()

	body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
	if err != nil {
		slog.ErrorContext(ctx, "read webhook body", "error", err.Error())
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var payload alertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.ErrorContext(ctx, "decode alertmanager payload", "error", err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.InfoContext(ctx, "webhook received",
		"status", payload.Status,
		"alert_count", len(payload.Alerts),
	)

	span.SetAttributes(
		attribute.String("alert.status", payload.Status),
		attribute.Int("alert.count", len(payload.Alerts)),
	)

	// Process each alert independently. If one fails the others still run.
	for _, a := range payload.Alerts {
		// Only process firing alerts. Resolved alerts do not need diagnosis.
		if a.Status != "firing" {
			continue
		}
		h.processAlert(ctx, a)
	}

	w.WriteHeader(http.StatusOK)
}

// processAlert runs the full pipeline for one alert:
//  1. Collect evidence (Loki + Prometheus).
//  2. Run LLM diagnosis.
//  3. Start Temporal IncidentWorkflow with the diagnosis attached.
func (h *Handler) processAlert(ctx context.Context, a alertItem) {
	alertName := a.Labels["alertname"]
	severity := a.Labels["severity"]
	if severity == "" {
		severity = "warning"
	}
	summary := a.Annotations["summary"]
	if summary == "" {
		summary = alertName
	}
	description := a.Annotations["description"]
	service := a.Labels["service"]
	if service == "" {
		service = "devops-sentinel-backend"
	}

	_, span := otel.Tracer(tracerName).Start(ctx, "webhook.processAlert")
	defer span.End()
	span.SetAttributes(
		attribute.String("alert.name", alertName),
		attribute.String("alert.severity", severity),
	)

	slog.InfoContext(ctx, "processing alert",
		"alertName", alertName,
		"severity", severity,
	)

	// Step 1: Collect evidence with a generous timeout.
	evCtx, evCancel := context.WithTimeout(ctx, 30*time.Second)
	ev := h.Collector.Collect(evCtx, alertName, service)
	evCancel()

	slog.InfoContext(ctx, "evidence collected",
		"alertName", alertName,
		"logLines", ev.LogLineCount,
	)

	// Step 2: Run LLM diagnosis.
	// The evidence has already been redacted by the collector.
	diagCtx, diagCancel := context.WithTimeout(ctx, 120*time.Second)
	diagnosis, err := h.LLM.Diagnose(diagCtx, llm.DiagnoseRequest{
		AlertName:      alertName,
		Severity:       severity,
		Summary:        summary,
		LogExcerpt:     ev.LogExcerpt,
		MetricsSummary: ev.MetricsSummary,
	})
	diagCancel()

	if err != nil {
		// Diagnose() already returns a Fallback on error, so this path is
		// only reached if something unexpected happens outside the provider.
		slog.ErrorContext(ctx, "LLM diagnosis failed unexpectedly", "error", err.Error())
		diagnosis = llm.Fallback("unexpected error: " + err.Error())
	}

	slog.InfoContext(ctx, "diagnosis complete",
		"alertName", alertName,
		"confidence", diagnosis.Confidence,
		"proposedAction", diagnosis.ProposedAction,
	)

	span.SetAttributes(
		attribute.String("diagnosis.confidence", diagnosis.Confidence),
		attribute.String("diagnosis.proposed_action", diagnosis.ProposedAction),
	)

	// Step 3: Start the Temporal IncidentWorkflow with the diagnosis embedded.
	// The workflow (from Step 7) will then:
	//   - Send the diagnosis to Slack/WhatsApp asking for approve/reject/ignore.
	//   - Wait for the human signal.
	//   - Record the decision and (in a future step) apply the action.
	workflowID := fmt.Sprintf("incident-%s-%d", alertName, a.StartsAt.Unix())

	input := incidentInput{
		AlertName:        alertName,
		Severity:         severity,
		Summary:          summary,
		Description:      buildDescription(description, diagnosis),
		StartsAt:         a.StartsAt.Format(time.RFC3339),
		DiagnosisSummary: diagnosis.Summary,
		RootCause:        diagnosis.RootCause,
		Confidence:       diagnosis.Confidence,
		ProposedAction:   diagnosis.ProposedAction,
		ActionRationale:  diagnosis.ActionRationale,
		EvidenceUsed:     diagnosis.EvidenceUsed,
	}

	wfCtx, wfCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wfCancel()

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: h.TaskQueue,
		// If the same alert fires again while a workflow is already running,
		// do not start a duplicate. The existing workflow handles it.
		WorkflowIDReusePolicy: 2, // WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
	}

	we, err := h.TemporalClient.ExecuteWorkflow(wfCtx, opts, "IncidentWorkflow", input)
	if err != nil {
		slog.ErrorContext(ctx, "failed to start IncidentWorkflow",
			"alertName", alertName,
			"workflowID", workflowID,
			"error", err.Error(),
		)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	slog.InfoContext(ctx, "IncidentWorkflow started",
		"alertName", alertName,
		"workflowID", workflowID,
		"runID", we.GetRunID(),
	)

	span.SetAttributes(
		attribute.String("temporal.workflow_id", workflowID),
		attribute.String("temporal.run_id", we.GetRunID()),
	)
}

// buildDescription combines the original alert description with the LLM diagnosis
// into a single string that the Temporal workflow passes to the notification activity.
func buildDescription(original string, d llm.Diagnosis) string {
	parts := []string{}
	if original != "" {
		parts = append(parts, "*Alert description:* "+original)
	}
	parts = append(parts,
		"*AI diagnosis:* "+d.Summary,
		"*Root cause:* "+d.RootCause,
		"*Confidence:* "+d.Confidence,
		"*Proposed action:* "+d.ProposedAction,
		"*Rationale:* "+d.ActionRationale,
	)
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}
