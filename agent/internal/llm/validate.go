// internal/llm/validate.go
// Validates and parses the raw JSON string returned by the LLM.
//
// Validation rules:
//   1. The string must be valid JSON.
//   2. All required fields must be present and non-empty.
//   3. confidence must be one of: low, medium, high.
//   4. proposed_action must be one of the AllowedActions values.
//   5. evidence_used must be a non-nil slice (may be empty if no evidence found).
//
// If validation fails the caller should retry once with the same prompt.
// After two failures the caller uses Fallback().

package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationError describes why a Diagnosis failed validation.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed on field %q: %s", e.Field, e.Message)
}

// ParseAndValidate parses raw LLM output into a Diagnosis and validates it.
// Returns a ValidationError if the output does not conform to the schema.
func ParseAndValidate(raw string) (Diagnosis, error) {
	// Strip any markdown code fences the LLM may have added despite instructions.
	raw = stripFences(raw)
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return Diagnosis{}, &ValidationError{Field: "<root>", Message: "LLM returned an empty response"}
	}

	var d Diagnosis
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Diagnosis{}, &ValidationError{
			Field:   "<root>",
			Message: fmt.Sprintf("invalid JSON: %s", err.Error()),
		}
	}

	// Required non-empty string fields.
	for _, check := range []struct{ field, value string }{
		{"summary", d.Summary},
		{"root_cause", d.RootCause},
		{"confidence", d.Confidence},
		{"proposed_action", d.ProposedAction},
		{"action_rationale", d.ActionRationale},
	} {
		if strings.TrimSpace(check.value) == "" {
			return Diagnosis{}, &ValidationError{
				Field:   check.field,
				Message: "field is required and must not be empty",
			}
		}
	}

	// Enum validation: confidence.
	if !AllowedConfidences[strings.ToLower(d.Confidence)] {
		return Diagnosis{}, &ValidationError{
			Field:   "confidence",
			Message: fmt.Sprintf("must be one of low|medium|high, got %q", d.Confidence),
		}
	}
	d.Confidence = strings.ToLower(d.Confidence)

	// Enum validation: proposed_action.
	if !AllowedActions[strings.ToLower(d.ProposedAction)] {
		return Diagnosis{}, &ValidationError{
			Field:   "proposed_action",
			Message: fmt.Sprintf("must be one of restart_deployment|scale_deployment|rollback_deployment|no_action, got %q", d.ProposedAction),
		}
	}
	d.ProposedAction = strings.ToLower(d.ProposedAction)

	// evidence_used must not be nil (may be empty slice).
	if d.EvidenceUsed == nil {
		d.EvidenceUsed = []string{}
	}

	return d, nil
}

// stripFences removes markdown code fences that some LLMs add despite
// being told not to. Handles ```json ... ``` and ``` ... ```.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			// Remove the closing fence if present.
			if idx := strings.LastIndex(s, "```"); idx >= 0 {
				s = s[:idx]
			}
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}
