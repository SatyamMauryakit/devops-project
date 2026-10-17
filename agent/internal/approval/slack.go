// internal/approval/slack.go
//
// Handles Slack interactive message callbacks (button clicks).
//
// When the Temporal workflow sends a notification via the notify activity,
// it includes an incident ID in the Slack message. The Slack message has
// "Approve" and "Reject" buttons. When a user clicks one, Slack POSTs to
// POST /approval/slack with the action payload.
//
// Security:
//   Slack signs every request with HMAC-SHA256 using the app's signing secret.
//   We verify the signature before processing the payload.
//   Reference: https://api.slack.com/authentication/verifying-requests-from-slack
//
// Environment variable required:
//   SLACK_SIGNING_SECRET — from Slack App settings > Basic Information.

package approval

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SlackHandler processes interactive button callbacks from Slack.
type SlackHandler struct {
	// SigningSecret is the Slack app signing secret (from App settings).
	// Never log this value.
	SigningSecret string

	// Dispatcher routes the decision to the correct Temporal workflow.
	Dispatcher Dispatcher
}

// Dispatcher sends a decision signal to a Temporal workflow.
// Implemented by the approval.TemporalDispatcher in main.go.
type Dispatcher interface {
	// Signal sends the decision ("approve" or "reject") to the workflow
	// identified by workflowID. Returns an error if the workflow is not found
	// or if a decision has already been recorded for this incident.
	Signal(workflowID, decision, approvedBy string) error
}

// slackInteractionPayload is a minimal subset of the Slack interactive payload.
// Full spec: https://api.slack.com/reference/interaction-payloads/block-actions
type slackInteractionPayload struct {
	Type string `json:"type"`
	User struct {
		ID   string `json:"id"`
		Name string `json:"username"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

// ServeHTTP implements http.Handler.
func (h *SlackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Verify the Slack request signature before doing anything with the body.
	if err := h.verifySignature(r.Header, body); err != nil {
		slog.Warn("slack signature verification failed", "error", err.Error())
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Slack sends the payload as a URL-encoded form field named "payload".
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	rawPayload := form.Get("payload")
	if rawPayload == "" {
		http.Error(w, "missing payload", http.StatusBadRequest)
		return
	}

	var payload slackInteractionPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		slog.Error("decode slack payload", "error", err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if len(payload.Actions) == 0 {
		// No actions — Slack sent a ping or an unsupported interaction type.
		w.WriteHeader(http.StatusOK)
		return
	}

	action := payload.Actions[0]
	// action_id is "approve_<workflowID>" or "reject_<workflowID>".
	// value carries the decision: "approve" or "reject".
	decision := strings.ToLower(action.Value)
	workflowID := extractWorkflowID(action.ActionID)

	if workflowID == "" {
		slog.Warn("slack action missing workflow ID", "action_id", action.ActionID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if decision != "approve" && decision != "reject" {
		slog.Warn("unknown slack decision", "decision", decision)
		w.WriteHeader(http.StatusOK)
		return
	}

	approvedBy := payload.User.Name
	if approvedBy == "" {
		approvedBy = payload.User.ID
	}

	slog.Info("slack decision received",
		"workflowID", workflowID,
		"decision", decision,
		"user", approvedBy,
	)

	if err := h.Dispatcher.Signal(workflowID, decision, approvedBy); err != nil {
		slog.Error("signal workflow", "workflowID", workflowID, "error", err.Error())
		// Return 200 to Slack anyway so it does not show an error to the user.
		// The error is already logged for the operator to investigate.
	}

	// Acknowledge the button click with an updated message.
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"response_action":"update","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Decision recorded: *%s* by %s"}}]}`,
		strings.ToUpper(decision), approvedBy)
}

// verifySignature checks the X-Slack-Signature header against the request body.
// Slack uses HMAC-SHA256 with the signing secret as the key.
// The message is: "v0:<timestamp>:<body>"
func (h *SlackHandler) verifySignature(headers http.Header, body []byte) error {
	if h.SigningSecret == "" {
		return fmt.Errorf("SLACK_SIGNING_SECRET is not configured")
	}

	timestamp := headers.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("missing X-Slack-Request-Timestamp header")
	}

	// Reject requests older than 5 minutes to prevent replay attacks.
	ts, err := parseTimestamp(timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if time.Since(ts) > 5*time.Minute {
		return fmt.Errorf("request timestamp too old: %s", timestamp)
	}

	sigHeader := headers.Get("X-Slack-Signature")
	if sigHeader == "" {
		return fmt.Errorf("missing X-Slack-Signature header")
	}

	// Compute expected signature.
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(h.SigningSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Use hmac.Equal for constant-time comparison.
	if !hmac.Equal([]byte(expected), []byte(sigHeader)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// extractWorkflowID parses the workflowID from an action_id of the form
// "approve_<workflowID>" or "reject_<workflowID>".
func extractWorkflowID(actionID string) string {
	for _, prefix := range []string{"approve_", "reject_"} {
		if strings.HasPrefix(actionID, prefix) {
			return strings.TrimPrefix(actionID, prefix)
		}
	}
	return ""
}

// parseTimestamp parses a Unix timestamp string (seconds since epoch).
func parseTimestamp(s string) (time.Time, error) {
	var secs int64
	_, err := fmt.Sscanf(s, "%d", &secs)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(secs, 0), nil
}

// ---------------------------------------------------------------------------
// BuildSlackApprovalBlocks constructs the Block Kit payload for the approval
// message. Called by the notify activity when sending the initial alert.
// workflowID is embedded in the action_id so the handler can route the click.
// ---------------------------------------------------------------------------

// ApprovalBlock is the JSON-serialisable Block Kit payload.
type ApprovalBlock struct {
	Blocks []map[string]interface{} `json:"blocks"`
}

// BuildApprovalBlocks creates an interactive Slack message with Approve and
// Reject buttons for the given incident.
func BuildApprovalBlocks(workflowID, title, body string) ApprovalBlock {
	return ApprovalBlock{
		Blocks: []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]string{
					"type": "plain_text",
					"text": "🚨 " + title,
				},
			},
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": body,
				},
			},
			{
				"type": "actions",
				"elements": []map[string]interface{}{
					{
						"type":      "button",
						"style":     "primary",
						"action_id": "approve_" + workflowID,
						"value":     "approve",
						"text": map[string]string{
							"type": "plain_text",
							"text": "Approve",
						},
						"confirm": map[string]interface{}{
							"title":   map[string]string{"type": "plain_text", "text": "Confirm approval"},
							"text":    map[string]string{"type": "mrkdwn", "text": "This will apply the proposed remediation action."},
							"confirm": map[string]string{"type": "plain_text", "text": "Yes, approve"},
							"deny":    map[string]string{"type": "plain_text", "text": "Cancel"},
						},
					},
					{
						"type":      "button",
						"style":     "danger",
						"action_id": "reject_" + workflowID,
						"value":     "reject",
						"text": map[string]string{
							"type": "plain_text",
							"text": "Reject",
						},
					},
				},
			},
			{"type": "divider"},
		},
	}
}
