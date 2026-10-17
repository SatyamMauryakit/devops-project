// internal/llm/provider.go
// Defines the Provider interface and the Diagnosis struct.
//
// Every LLM implementation (Ollama, Groq, etc.) must satisfy Provider.
// The rest of the agent only depends on this interface, never on a
// concrete implementation — making it easy to swap providers at runtime.

package llm

import "context"

// AllowedActions is the fixed set of actions the LLM is permitted to propose.
// Any value outside this set fails validation and triggers the fallback.
var AllowedActions = map[string]bool{
	"restart_deployment":  true,
	"scale_deployment":    true,
	"rollback_deployment": true,
	"no_action":           true,
}

// AllowedConfidences is the fixed set of valid confidence values.
var AllowedConfidences = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

// Diagnosis is the structured output the LLM must return.
// The JSON field names are part of the contract with the LLM prompt —
// changing them requires updating the prompt in each provider.
type Diagnosis struct {
	// Summary is a 1-2 sentence plain-English description of the incident.
	Summary string `json:"summary"`

	// RootCause is the LLM's best guess at what caused the alert.
	RootCause string `json:"root_cause"`

	// Confidence is how confident the LLM is in its diagnosis.
	// Must be one of: low, medium, high.
	Confidence string `json:"confidence"`

	// EvidenceUsed lists the specific log lines or metric values that
	// informed the diagnosis. These must come from the evidence passed
	// in the prompt, not fabricated by the LLM.
	EvidenceUsed []string `json:"evidence_used"`

	// ProposedAction is the single remediation action the LLM recommends.
	// Must be one of the values in AllowedActions.
	ProposedAction string `json:"proposed_action"`

	// ActionRationale explains why this action was chosen.
	ActionRationale string `json:"action_rationale"`
}

// Fallback returns a safe default Diagnosis used when the LLM fails or
// returns invalid output after retries.
func Fallback(reason string) Diagnosis {
	return Diagnosis{
		Summary:         "Automated diagnosis unavailable: " + reason,
		RootCause:       "Unknown — LLM did not return a valid response.",
		Confidence:      "low",
		EvidenceUsed:    []string{},
		ProposedAction:  "no_action",
		ActionRationale: "Fallback to no_action because automated diagnosis failed. Escalate to human review.",
	}
}

// Provider is the interface all LLM backends must implement.
type Provider interface {
	// Diagnose sends the incident evidence to the LLM and returns a
	// structured Diagnosis. The caller is responsible for redacting
	// secrets from the evidence before calling this method.
	Diagnose(ctx context.Context, req DiagnoseRequest) (Diagnosis, error)
}

// DiagnoseRequest holds the evidence and context sent to the LLM.
type DiagnoseRequest struct {
	// AlertName is the Prometheus alert name, e.g. "BackendDown".
	AlertName string
	// Severity is the alert severity label.
	Severity string
	// Summary is the alert summary annotation.
	Summary string
	// LogExcerpt is the redacted log lines from Loki (max 2000 chars).
	LogExcerpt string
	// MetricsSummary is the formatted metrics snapshot from Prometheus.
	MetricsSummary string
}

// BuildPrompt constructs the LLM prompt from a DiagnoseRequest.
// It is a pure function so it can be unit-tested without a real LLM.
// The prompt uses a JSON schema fence to guide the model's output format.
func BuildPrompt(req DiagnoseRequest) string {
	return `You are a senior site reliability engineer diagnosing a production incident.

## Incident

Alert: ` + req.AlertName + `
Severity: ` + req.Severity + `
Summary: ` + req.Summary + `

## Recent Error Logs (last 15 minutes, secrets redacted)

` + req.LogExcerpt + `

## Current Metrics

` + req.MetricsSummary + `

## Instructions

Analyse the evidence above and respond with ONLY a valid JSON object matching
this exact schema. Do not include any text outside the JSON object.

{
  "summary":          "<1-2 sentence description of what is happening>",
  "root_cause":       "<most likely cause based on the evidence>",
  "confidence":       "<one of: low, medium, high>",
  "evidence_used":    ["<exact log line or metric that supports your diagnosis>"],
  "proposed_action":  "<one of: restart_deployment, scale_deployment, rollback_deployment, no_action>",
  "action_rationale": "<why you chose this action>"
}

Rules:
- proposed_action MUST be exactly one of the four allowed values.
- confidence MUST be exactly one of: low, medium, high.
- evidence_used MUST only contain items from the logs or metrics above.
- Do not invent evidence. If the logs are empty, say so in root_cause.
- Respond with JSON only. No markdown fences, no preamble, no explanation.`
}
