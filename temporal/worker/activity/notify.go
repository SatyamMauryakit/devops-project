// activity/notify.go
// SendNotification activity — fans out to Slack and WhatsApp.
//
// This is intentionally structurally equivalent to the notifier service in
// Step 6 so the two implementations stay in sync. The difference is that
// this version is called as a Temporal activity, which means it benefits from
// automatic retries with exponential backoff.

package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const NotifyActivityName = "SendNotification"

// NotifyActivity holds the credentials for both notification channels.
// All fields are injected at worker startup from environment variables.
type NotifyActivity struct {
	SlackWebhookURL       string
	WhatsAppAccessToken   string
	WhatsAppPhoneNumberID string
	WhatsAppRecipient     string
	WhatsAppTemplateName  string
	WhatsAppTemplateLang  string
	HTTPClient            *http.Client
}

func (a *NotifyActivity) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// SendNotificationInput is the parameter type for the activity.
type SendNotificationInput struct {
	Title        string
	Body         string
	WhatsAppText string
	Color        string // "good" | "warning" | "danger"
}

// SendNotificationResult carries the outcome of each channel.
type SendNotificationResult struct {
	SlackSent     bool
	WhatsAppSent  bool
	SlackError    string
	WhatsAppError string
}

// SendNotification fans out to Slack and WhatsApp concurrently and returns
// a summary. Partial failures are returned in the result, not as errors,
// so the workflow can continue even if one channel is down.
func (a *NotifyActivity) SendNotification(ctx context.Context, input SendNotificationInput) (SendNotificationResult, error) {
	var (
		result SendNotificationResult
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		err := a.sendSlack(ctx, input.Title, input.Body, input.Color)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			result.SlackError = err.Error()
		} else {
			result.SlackSent = true
		}
	}()

	go func() {
		defer wg.Done()
		err := a.sendWhatsApp(ctx, input.WhatsAppText)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			result.WhatsAppError = err.Error()
		} else {
			result.WhatsAppSent = true
		}
	}()

	wg.Wait()
	return result, nil
}

// ---------------------------------------------------------------------------
// Slack Block Kit sender
// ---------------------------------------------------------------------------

type slackPayload struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (a *NotifyActivity) sendSlack(ctx context.Context, title, body, color string) error {
	if a.SlackWebhookURL == "" {
		return nil // channel not configured
	}

	icon := "✅"
	switch color {
	case "danger":
		icon = "🚨"
	case "warning":
		icon = "⚠️"
	}

	payload := slackPayload{
		Blocks: []slackBlock{
			{Type: "header", Text: &slackText{Type: "plain_text", Text: icon + " " + title}},
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: body}},
			{Type: "divider"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.SlackWebhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("slack request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// WhatsApp Cloud API sender (template message)
// ---------------------------------------------------------------------------

type waMessage struct {
	MessagingProduct string      `json:"messaging_product"`
	To               string      `json:"to"`
	Type             string      `json:"type"`
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

func (a *NotifyActivity) sendWhatsApp(ctx context.Context, text string) error {
	if a.WhatsAppAccessToken == "" || a.WhatsAppPhoneNumberID == "" || a.WhatsAppRecipient == "" {
		return nil // channel not configured
	}

	msg := waMessage{
		MessagingProduct: "whatsapp",
		To:               a.WhatsAppRecipient,
		Type:             "template",
		Template: &waTemplate{
			Name:     a.WhatsAppTemplateName,
			Language: waLang{Code: a.WhatsAppTemplateLang},
			Components: []waComponent{
				{
					Type:       "body",
					Parameters: []waParameter{{Type: "text", Text: text}},
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal whatsapp: %w", err)
	}

	reqURL := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/messages", a.WhatsAppPhoneNumberID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build whatsapp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.WhatsAppAccessToken)

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp request: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp %d: %s", resp.StatusCode, b)
	}
	return nil
}
