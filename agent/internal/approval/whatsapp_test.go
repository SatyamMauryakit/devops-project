package approval_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devops-sentinel/agent/internal/approval"
)

const (
	waTestAppSecret   = "test-meta-app-secret-xyz"
	waTestVerifyToken = "test-verify-token-xyz"
)

// signedWARequest creates a POST request with a valid Meta HMAC-SHA256 signature.
func signedWARequest(t *testing.T, secret string, body []byte) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/approval/whatsapp",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)
	return req
}

// buildWAPayload builds a minimal Meta webhook payload with one text message.
func buildWAPayload(from, text string) []byte {
	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123456",
				"changes": []map[string]interface{}{
					{
						"field": "messages",
						"value": map[string]interface{}{
							"messages": []map[string]interface{}{
								{
									"from": from,
									"type": "text",
									"text": map[string]string{"body": text},
								},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func newWAHandler(disp approval.Dispatcher) *approval.WhatsAppHandler {
	return &approval.WhatsAppHandler{
		AppSecret:   waTestAppSecret,
		VerifyToken: waTestVerifyToken,
		Dispatcher:  disp,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWhatsAppHandler_ApproveMessage(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	body := buildWAPayload("+12025550123", "approve incident-BackendDown-1726560000")
	req := signedWARequest(t, waTestAppSecret, body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if disp.lastDecision != "approve" {
		t.Errorf("expected approve, got %q", disp.lastDecision)
	}
	if disp.lastWorkflowID != "incident-BackendDown-1726560000" {
		t.Errorf("expected workflow ID, got %q", disp.lastWorkflowID)
	}
	if !strings.HasPrefix(disp.lastUser, "whatsapp:") {
		t.Errorf("expected whatsapp: prefix in user, got %q", disp.lastUser)
	}
}

func TestWhatsAppHandler_RejectMessage(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	body := buildWAPayload("+12025550124", "reject incident-HighErrorRate-999")
	req := signedWARequest(t, waTestAppSecret, body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if disp.lastDecision != "reject" {
		t.Errorf("expected reject, got %q", disp.lastDecision)
	}
	if disp.lastWorkflowID != "incident-HighErrorRate-999" {
		t.Errorf("expected correct workflow ID, got %q", disp.lastWorkflowID)
	}
}

func TestWhatsAppHandler_InvalidSignature(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	body := buildWAPayload("+1", "approve incident-Test-1")
	req := signedWARequest(t, "wrong-secret", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if disp.lastWorkflowID != "" {
		t.Error("Signal must not be called on bad signature")
	}
}

func TestWhatsAppHandler_WebhookVerification(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	url := fmt.Sprintf("/approval/whatsapp?hub.mode=subscribe&hub.verify_token=%s&hub.challenge=abc123",
		waTestVerifyToken)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for verification, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "abc123") {
		t.Errorf("expected challenge in response, got %q", rr.Body.String())
	}
}

func TestWhatsAppHandler_WrongVerifyToken(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	req := httptest.NewRequest(http.MethodGet,
		"/approval/whatsapp?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=xyz",
		nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWhatsAppHandler_NonDecisionMessage(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	body := buildWAPayload("+1", "hello there, how are you?")
	req := signedWARequest(t, waTestAppSecret, body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if disp.lastWorkflowID != "" {
		t.Error("Signal must not be called for non-decision message")
	}
}

func TestWhatsAppHandler_CaseInsensitiveDecision(t *testing.T) {
	disp := &mockDispatcher{}
	h := newWAHandler(disp)

	// User types "APPROVE" in caps.
	body := buildWAPayload("+1", "APPROVE incident-Test-42")
	req := signedWARequest(t, waTestAppSecret, body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if disp.lastDecision != "approve" {
		t.Errorf("expected normalized 'approve', got %q", disp.lastDecision)
	}
}
