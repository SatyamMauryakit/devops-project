// internal/approval/whatsapp.go
//
// Handles incoming WhatsApp message webhooks from Meta.
//
// Flow:
//  1. User receives a WhatsApp template message containing the incident ID.
//  2. User replies with "approve <incidentID>" or "reject <incidentID>".
//  3. Meta POSTs the reply to POST /approval/whatsapp.
//  4. This handler verifies the X-Hub-Signature-256 header, extracts the
//     decision and incident ID, and signals the Temporal workflow.
//
// Security:
//  Meta signs every webhook delivery with HMAC-SHA256 using the app secret.
//  The signature is in the header: X-Hub-Signature-256: sha256=<hex>
//  Reference: https://developers.facebook.com/docs/graph-api/webhooks/getting-started#verification-requests
//
// Environment variables required:
//  WHATSAPP_APP_SECRET     — Meta app secret (App settings > Basic).
//  WHATSAPP_VERIFY_TOKEN   — Any string you choose; used for webhook registration.
//
// Incident ID mapping:
//  The template message body is: "DevOps Sentinel alert: <incidentID>: <summary>"
//  When the user replies, their message should be:
//    "approve incident-BackendDown-1726560000"
//    "reject incident-BackendDown-1726560000"
//  The handler parses the first word as the decision and the second as the ID.

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
	"strings"
)

// WhatsAppHandler processes incoming WhatsApp message webhooks.
type WhatsAppHandler struct {
	// AppSecret is the Meta app secret used to verify the request signature.
	AppSecret string
	// VerifyToken is used during webhook registration with Meta.
	VerifyToken string
	// Dispatcher routes decisions to Temporal workflows.
	Dispatcher Dispatcher
}

// ServeHTTP implements http.Handler.
// Handles both GET (webhook verification) and POST (incoming messages).
func (h *WhatsAppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleVerification(w, r)
	case http.MethodPost:
		h.handleMessage(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerification responds to Meta's webhook verification challenge.
// Meta sends a GET with hub.challenge when you register the webhook URL.
func (h *WhatsAppHandler) handleVerification(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.mode") == "subscribe" &&
		q.Get("hub.verify_token") == h.VerifyToken {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, q.Get("hub.challenge"))
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}

// handleMessage processes an incoming WhatsApp message notification.
func (h *WhatsAppHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if err := h.verifySignature(r.Header, body); err != nil {
		slog.Warn("whatsapp signature verification failed", "error", err.Error())
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload waWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("decode whatsapp payload", "error", err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Walk the nested payload to find incoming text messages.
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" {
					continue
				}
				h.processTextMessage(msg.Text.Body, msg.From)
			}
		}
	}

	// Always return 200 quickly. Meta will retry if we return non-2xx.
	w.WriteHeader(http.StatusOK)
}

// processTextMessage parses a reply and routes the decision.
// Expected format: "approve <workflowID>" or "reject <workflowID>"
func (h *WhatsAppHandler) processTextMessage(text, from string) {
	text = strings.TrimSpace(strings.ToLower(text))
	parts := strings.Fields(text)

	if len(parts) < 2 {
		slog.Debug("whatsapp message not a decision command",
			"text", text, "from", from)
		return
	}

	decision := parts[0]
	workflowID := parts[1]

	if decision != "approve" && decision != "reject" {
		slog.Debug("whatsapp message not approve/reject",
			"first_word", decision, "from", from)
		return
	}

	slog.Info("whatsapp decision received",
		"workflowID", workflowID,
		"decision", decision,
		"from", from,
	)

	if err := h.Dispatcher.Signal(workflowID, decision, "whatsapp:"+from); err != nil {
		slog.Error("signal workflow from whatsapp",
			"workflowID", workflowID,
			"error", err.Error(),
		)
	}
}

// verifySignature checks the X-Hub-Signature-256 header.
// Meta computes: HMAC-SHA256(appSecret, rawBody) and sends it as
// "sha256=<hex>" in the X-Hub-Signature-256 header.
func (h *WhatsAppHandler) verifySignature(headers http.Header, body []byte) error {
	if h.AppSecret == "" {
		return fmt.Errorf("WHATSAPP_APP_SECRET is not configured")
	}

	sigHeader := headers.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		return fmt.Errorf("missing X-Hub-Signature-256 header")
	}

	if !strings.HasPrefix(sigHeader, "sha256=") {
		return fmt.Errorf("unexpected signature format: %s", sigHeader)
	}
	sigHex := strings.TrimPrefix(sigHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.AppSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sigHex)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Meta webhook payload types (minimal subset)
// ---------------------------------------------------------------------------

type waWebhookPayload struct {
	Object string     `json:"object"`
	Entry  []waEntry  `json:"entry"`
}

type waEntry struct {
	ID      string     `json:"id"`
	Changes []waChange `json:"changes"`
}

type waChange struct {
	Value waChangeValue `json:"value"`
	Field string        `json:"field"`
}

type waChangeValue struct {
	Messages []waMessage `json:"messages"`
}

type waMessage struct {
	From string `json:"from"`
	Type string `json:"type"`
	Text struct {
		Body string `json:"body"`
	} `json:"text"`
}
