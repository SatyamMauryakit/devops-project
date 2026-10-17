package llm_test

import (
	"strings"
	"testing"

	"github.com/devops-sentinel/agent/internal/llm"
)

// validJSON returns a well-formed Diagnosis JSON string.
func validJSON() string {
	return `{
		"summary": "The backend deployment has zero ready replicas.",
		"root_cause": "All pods failed liveness probes after a bad deploy.",
		"confidence": "high",
		"evidence_used": ["ready replicas: 0", "restart count: 5"],
		"proposed_action": "restart_deployment",
		"action_rationale": "Restarting the deployment will recreate the pods with the current image."
	}`
}

func TestParseAndValidate_ValidInput(t *testing.T) {
	d, err := llm.ParseAndValidate(validJSON())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ProposedAction != "restart_deployment" {
		t.Errorf("expected restart_deployment, got %q", d.ProposedAction)
	}
	if d.Confidence != "high" {
		t.Errorf("expected high, got %q", d.Confidence)
	}
	if len(d.EvidenceUsed) != 2 {
		t.Errorf("expected 2 evidence items, got %d", len(d.EvidenceUsed))
	}
}

func TestParseAndValidate_StripsMarkdownFence(t *testing.T) {
	fenced := "```json\n" + validJSON() + "\n```"
	d, err := llm.ParseAndValidate(fenced)
	if err != nil {
		t.Fatalf("unexpected error stripping fence: %v", err)
	}
	if d.Summary == "" {
		t.Error("summary should not be empty after fence strip")
	}
}

func TestParseAndValidate_InvalidJSON(t *testing.T) {
	_, err := llm.ParseAndValidate("this is not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' in error, got: %v", err)
	}
}

func TestParseAndValidate_EmptyResponse(t *testing.T) {
	_, err := llm.ParseAndValidate("   ")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestParseAndValidate_MissingRequiredField(t *testing.T) {
	// Missing "summary" field.
	input := `{
		"root_cause": "something",
		"confidence": "low",
		"evidence_used": [],
		"proposed_action": "no_action",
		"action_rationale": "fallback"
	}`
	_, err := llm.ParseAndValidate(input)
	if err == nil {
		t.Fatal("expected error for missing summary")
	}
	if !strings.Contains(err.Error(), "summary") {
		t.Errorf("expected 'summary' in error, got: %v", err)
	}
}

func TestParseAndValidate_InvalidConfidence(t *testing.T) {
	input := `{
		"summary": "test",
		"root_cause": "test",
		"confidence": "very_sure",
		"evidence_used": [],
		"proposed_action": "no_action",
		"action_rationale": "test"
	}`
	_, err := llm.ParseAndValidate(input)
	if err == nil {
		t.Fatal("expected error for invalid confidence")
	}
	if !strings.Contains(err.Error(), "confidence") {
		t.Errorf("expected 'confidence' in error, got: %v", err)
	}
}

func TestParseAndValidate_InvalidAction(t *testing.T) {
	input := `{
		"summary": "test",
		"root_cause": "test",
		"confidence": "medium",
		"evidence_used": [],
		"proposed_action": "delete_everything",
		"action_rationale": "test"
	}`
	_, err := llm.ParseAndValidate(input)
	if err == nil {
		t.Fatal("expected error for invalid proposed_action")
	}
	if !strings.Contains(err.Error(), "proposed_action") {
		t.Errorf("expected 'proposed_action' in error, got: %v", err)
	}
}

func TestParseAndValidate_CaseNormalization(t *testing.T) {
	// LLM may return uppercase confidence and action — should be normalized.
	input := `{
		"summary": "test incident",
		"root_cause": "unknown",
		"confidence": "HIGH",
		"evidence_used": [],
		"proposed_action": "RESTART_DEPLOYMENT",
		"action_rationale": "normalized"
	}`
	d, err := llm.ParseAndValidate(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Confidence != "high" {
		t.Errorf("expected normalized 'high', got %q", d.Confidence)
	}
	if d.ProposedAction != "restart_deployment" {
		t.Errorf("expected normalized 'restart_deployment', got %q", d.ProposedAction)
	}
}

func TestParseAndValidate_NilEvidenceBecomesEmptySlice(t *testing.T) {
	// LLM omits evidence_used field entirely.
	input := `{
		"summary": "test",
		"root_cause": "test",
		"confidence": "low",
		"proposed_action": "no_action",
		"action_rationale": "test"
	}`
	d, err := llm.ParseAndValidate(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.EvidenceUsed == nil {
		t.Error("EvidenceUsed should be empty slice, not nil")
	}
}

func TestBuildPrompt_ContainsAlertName(t *testing.T) {
	req := llm.DiagnoseRequest{
		AlertName:      "BackendDown",
		Severity:       "critical",
		Summary:        "All pods unready",
		LogExcerpt:     "connection refused",
		MetricsSummary: "ready replicas: 0",
	}
	prompt := llm.BuildPrompt(req)
	if !strings.Contains(prompt, "BackendDown") {
		t.Error("prompt should contain the alert name")
	}
	if !strings.Contains(prompt, "connection refused") {
		t.Error("prompt should contain the log excerpt")
	}
	if !strings.Contains(prompt, "proposed_action") {
		t.Error("prompt should contain the JSON schema instructions")
	}
}
