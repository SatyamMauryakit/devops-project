// main.go
// DevOps Sentinel - Notifier Service
//
// Responsibilities:
//  1. Expose POST /webhook  - receive Alertmanager webhook payloads.
//  2. Expose POST /test     - send a test message to both channels (manual trigger).
//  3. Expose GET  /health   - liveness probe.
//  4. Fan out to Slack (Block Kit) and WhatsApp (Cloud API template) concurrently.
//
// Every log line is structured JSON so it integrates with the Loki pipeline
// from Step 3. The active OTel span is not available here yet (that is Step 7),
// so trace_id is omitted for now — the field is present but empty to keep the
// log schema consistent.
//
// Configuration — all via environment variables (never hardcoded):
//
//   SLACK_WEBHOOK_URL          Incoming webhook URL from Slack App settings.
//   WHATSAPP_ACCESS_TOKEN      Meta Cloud API access token (expires every 24h for test tokens).
//   WHATSAPP_PHONE_NUMBER_ID   The FROM phone number ID in Meta developer console.
//   WHATSAPP_RECIPIENT_NUMBER  The TO phone number in E.164 format, e.g. +12025550123.
//   WHATSAPP_TEMPLATE_NAME     Approved template name, e.g. "sentinel_alert".
//   WHATSAPP_TEMPLATE_LANG     Template language code, e.g. "en_US".
//   PORT                       HTTP listen port (default: 8080).
//   LOG_LEVEL                  "debug" | "info" | "warn" | "error" (default: "info").

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func init() {
	// When the container HEALTHCHECK calls the binary with -health-check-only,
	// perform a quick HTTP GET to /health and exit 0 on 200, 1 otherwise.
	// This avoids needing curl or wget in the distroless image.
	for _, arg := range os.Args[1:] {
		if arg == "-health-check-only" {
			port := getEnv("PORT", "8080")
			resp, err := http.Get("http://localhost:" + port + "/health")
			if err != nil || resp.StatusCode != http.StatusOK {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

// ---------------------------------------------------------------------------
// Alertmanager webhook payload types
// ---------------------------------------------------------------------------

type alertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"` // "firing" | "resolved"
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []alert           `json:"alerts"`
}

type alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

type config struct {
	port                    string
	slackWebhookURL         string
	waAccessToken           string
	waPhoneNumberID         string
	waRecipientNumber       string
	waTemplateName          string
	waTemplateLang          string
}

func loadConfig() (config, error) {
	c := config{
		port:               getEnv("PORT", "8080"),
		slackWebhookURL:    os.Getenv("SLACK_WEBHOOK_URL"),
		waAccessToken:      os.Getenv("WHATSAPP_ACCESS_TOKEN"),
		waPhoneNumberID:    os.Getenv("WHATSAPP_PHONE_NUMBER_ID"),
		waRecipientNumber:  os.Getenv("WHATSAPP_RECIPIENT_NUMBER"),
		waTemplateName:     getEnv("WHATSAPP_TEMPLATE_NAME", "sentinel_alert"),
		waTemplateLang:     getEnv("WHATSAPP_TEMPLATE_LANG", "en_US"),
	}

	// Warn rather than hard-fail so the service starts even when one channel
	// is not yet configured. Messages to the missing channel are skipped.
	if c.slackWebhookURL == "" {
		slog.Warn("SLACK_WEBHOOK_URL is not set — Slack notifications disabled")
	}
	if c.waAccessToken == "" || c.waPhoneNumberID == "" || c.waRecipientNumber == "" {
		slog.Warn("WhatsApp env vars incomplete — WhatsApp notifications disabled")
	}
	return c, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Slack sender — Block Kit format
// ---------------------------------------------------------------------------

// slackBlock is a minimal representation of one Slack Block Kit block.
type slackBlock struct {
	Type string      `json:"type"`
	Text *slackText  `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackPayload struct {
	Blocks []slackBlock `json:"blocks"`
}

func sendSlack(ctx context.Context, webhookURL, title, body, color string) error {
	if webhookURL == "" {
		return nil // channel not configured
	}

	// Slack does not support colored Block Kit messages natively; color is
	// embedded in the header emoji for simple visual distinction.
	icon := "✅"
	if color == "danger" {
		icon = "🚨"
	} else if color == "warning" {
		icon = "⚠️"
	}

	payload := slackPayload{
		Blocks: []slackBlock{
			{
				Type: "header",
				Text: &slackText{Type: "plain_text", Text: icon + " " + title},
			},
			{
				Type: "section",
				Text: &slackText{Type: "mrkdwn", Text: body},
			},
			{
				Type: "divider",
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send slack request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack responded %d: %s", resp.StatusCode, body)
	}
	return nil
}

// ---------------------------------------------------------------------------
// WhatsApp sender — Meta Cloud API template message
// ---------------------------------------------------------------------------
//
// Why templates?
// The Meta WhatsApp Cloud API enforces a 24-hour customer service window.
// If the recipient has not sent a message to the test number within the last
// 24 hours, the API rejects free-form text with error code 131047.
// Pre-approved template messages (created in Meta Business Manager) can be
// sent at any time regardless of the window.
//
// For the test setup, create a template named "sentinel_alert" with one
// body variable {{1}} for the alert text. Approval takes minutes in test mode.
//
// Template message structure used here:
//   Body: "DevOps Sentinel alert: {{1}}"
//   Language: en_US

type waMessage struct {
	MessagingProduct string     `json:"messaging_product"`
	To               string     `json:"to"`
	Type             string     `json:"type"`
	Template         *waTemplate `json:"template,omitempty"`
}

type waTemplate struct {
	Name       string        `json:"name"`
	Language   waLang        `json:"language"`
	Components []waComponent `json:"components"`
}

type waLang struct {
	Code string `json:"code"`
}

type waComponent struct {
	Type       string        `json:"type"`
	Parameters []waParameter `json:"parameters"`
}

type waParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func sendWhatsApp(ctx context.Context, cfg config, text string) error {
	if cfg.waAccessToken == "" || cfg.waPhoneNumberID == "" || cfg.waRecipientNumber == "" {
		return nil // channel not configured
	}

	msg := waMessage{
		MessagingProduct: "whatsapp",
		To:               cfg.waRecipientNumber,
		Type:             "template",
		Template: &waTemplate{
			Name:     cfg.waTemplateName,
			Language: waLang{Code: cfg.waTemplateLang},
			Components: []waComponent{
				{
					Type: "body",
					Parameters: []waParameter{
						{Type: "text", Text: text},
					},
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal whatsapp payload: %w", err)
	}

	url := fmt.Sprintf(
		"https://graph.facebook.com/v19.0/%s/messages",
		cfg.waPhoneNumberID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build whatsapp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.waAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send whatsapp request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp api %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fan-out: send to both channels concurrently, collect errors
// ---------------------------------------------------------------------------

func notify(ctx context.Context, cfg config, title, slackBody, waText, color string) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := sendSlack(ctx, cfg.slackWebhookURL, title, slackBody, color); err != nil {
			slog.Error("slack notification failed", "error", err.Error())
		} else {
			slog.Info("slack notification sent", "title", title)
		}
	}()

	go func() {
		defer wg.Done()
		if err := sendWhatsApp(ctx, cfg, waText); err != nil {
			slog.Error("whatsapp notification failed", "error", err.Error())
		} else {
			slog.Info("whatsapp notification sent")
		}
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func webhookHandler(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload alertmanagerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			slog.Error("decode alertmanager payload", "error", err.Error())
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		slog.Info("webhook received",
			"status", payload.Status,
			"alerts", len(payload.Alerts),
			"receiver", payload.Receiver,
		)

		for _, a := range payload.Alerts {
			alertName := a.Labels["alertname"]
			summary := a.Annotations["summary"]
			if summary == "" {
				summary = alertName
			}
			description := a.Annotations["description"]
			severity := a.Labels["severity"]
			if severity == "" {
				severity = "warning"
			}

			status := strings.ToUpper(a.Status)
			color := "danger"
			if a.Status == "resolved" {
				color = "good"
			} else if severity == "warning" {
				color = "warning"
			}

			title := fmt.Sprintf("[%s] %s — %s", status, alertName, severity)

			slackBody := fmt.Sprintf(
				"*%s*\n%s\n\nStarted: %s",
				summary,
				description,
				a.StartsAt.Format(time.RFC3339),
			)

			waText := fmt.Sprintf("[%s] %s: %s", status, alertName, summary)

			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			notify(ctx, cfg, title, slackBody, waText, color)
			cancel()
		}

		w.WriteHeader(http.StatusOK)
	}
}

func testHandler(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		notify(
			ctx, cfg,
			"[TEST] DevOps Sentinel notifier test",
			"*This is a test notification.*\nIf you see this in Slack, the notifier is working correctly.",
			"DevOps Sentinel test: notifier is working correctly.",
			"warning",
		)

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"test sent"}`)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"status":"ok"}`)
}

// ---------------------------------------------------------------------------
// Structured JSON logging setup
// ---------------------------------------------------------------------------

func setupLogging() {
	level := slog.LevelInfo
	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Rename "msg" -> "message" to match the log schema from Step 3.
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})

	slog.SetDefault(slog.New(handler))
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	setupLogging()

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("load config", "error", err.Error())
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhookHandler(cfg))
	mux.HandleFunc("/test",    testHandler(cfg))
	mux.HandleFunc("/health",  healthHandler)

	srv := &http.Server{
		Addr:         ":" + cfg.port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGTERM / SIGINT.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("notifier starting",
			"port", cfg.port,
			"slack_configured", cfg.slackWebhookURL != "",
			"whatsapp_configured", cfg.waAccessToken != "",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err.Error())
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err.Error())
	}
	slog.Info("shutdown complete")
}
