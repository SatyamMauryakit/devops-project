package approval_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devops-sentinel/agent/internal/approval"
)

// mockDispatcher records the last signal sent.
type mockDispatcher struct {
	lastWorkflowID string
	lastDecision   string
	lastUser       string
	err            error
}

func (m *mockDispatcher) Signal(wid, decision, user string) error {
	m.lastWorkflowID = wid
	m.lastDecision = decision
	m.lastUser = user
	return m.err
}

const testSecret = "test-signing-secret-abc123"

// signedRequest builds a POST request with a valid Slack signature.
func signedRequest(t *testing.T, secret, body string) *http.Request {
	t.Helper()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	baseStr := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseStr))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/approval/slack",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)
	return req
}

// buildPayload builds a URL-encoded Slack interaction payload.
func buildPayload(actionID, value, userID, userName string) string {
	inner := fmt.Sprintf(`{"type":"block_actions","user":{"id":%q,"username":%q},"actions":[{"action_id":%q,"value":%q}]}`,
		userID, userName, actionID, value)
	return "payload=" + url.QueryEscape(inner)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSlackHandler_ApproveButton(t *testing.T) {
	disp := &mockDispatcher{}
	h := &approval.SlackHandler{SigningSecret: testSecret, Dispatcher: disp}

	body := buildPayload("approve_incident-BackendDown-123", "approve", "U001", "alice")
	req := signedRequest(t, testSecret, body)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if disp.lastDecision != "approve" {
		t.Errorf("expected approve, got %q", disp.lastDecision)
	}
	if disp.lastWorkflowID != "incident-BackendDown-123" {
		t.Errorf("expected incident-BackendDown-123, got %q", disp.lastWorkflowID)
	}
	if disp.lastUser != "alice" {
		t.Errorf("expected alice, got %q", disp.lastUser)
	}
}

func TestSlackHandler_RejectButton(t *testing.T) {
	disp := &mockDispatcher{}
	h := &approval.SlackHandler{SigningSecret: testSecret, Dispatcher: disp}

	body := buildPayload("reject_incident-HighErrorRate-456", "reject", "U002", "bob")
	req := signedRequest(t, testSecret, body)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if disp.lastDecision != "reject" {
		t.Errorf("expected reject, got %q", disp.lastDecision)
	}
}

func TestSlackHandler_InvalidSignature(t *testing.T) {
	disp := &mockDispatcher{}
	h := &approval.SlackHandler{SigningSecret: testSecret, Dispatcher: disp}

	// Build a request signed with the WRONG secret.
	body := buildPayload("approve_incident-X-999", "approve", "U001", "eve")
	req := signedRequest(t, "wrong-secret", body)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	// Signal must NOT have been called.
	if disp.lastWorkflowID != "" {
		t.Error("Signal should not be called on invalid signature")
	}
}

func TestSlackHandler_OldTimestamp(t *testing.T) {
	disp := &mockDispatcher{}
	h := &approval.SlackHandler{SigningSecret: testSecret, Dispatcher: disp}

	// Build a request with a timestamp 10 minutes in the past.
	oldTimestamp := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())
	bodyStr := buildPayload("approve_incident-Old-1", "approve", "U001", "charlie")
	baseStr := fmt.Sprintf("v0:%s:%s", oldTimestamp, bodyStr)
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(baseStr))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/approval/slack",
		strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", oldTimestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for old timestamp, got %d", rr.Code)
	}
}

func TestExtractWorkflowID(t *testing.T) {
	cases := []struct {
		actionID string
		want     string
	}{
		{"approve_incident-BackendDown-1726560000", "incident-BackendDown-1726560000"},
		{"reject_incident-HighErrorRate-1726560001", "incident-HighErrorRate-1726560001"},
		{"unknown_something", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := approval.ExtractWorkflowIDForTest(tc.actionID)
		if got != tc.want {
			t.Errorf("actionID=%q: want %q, got %q", tc.actionID, tc.want, got)
		}
	}
}

func TestBuildApprovalBlocks_ContainsWorkflowID(t *testing.T) {
	blocks := approval.BuildApprovalBlocks("incident-Test-999", "Test Alert", "body text")
	// Marshal to JSON and verify the workflowID appears in action_id values.
	found := false
	for _, b := range blocks.Blocks {
		if b["type"] == "actions" {
			elems, _ := b["elements"].([]map[string]interface{})
			for _, el := range elems {
				if id, ok := el["action_id"].(string); ok {
					if bytes.Contains([]byte(id), []byte("incident-Test-999")) {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Error("workflow ID not found in action blocks")
	}
}
