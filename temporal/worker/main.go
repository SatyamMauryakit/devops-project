// main.go
// DevOps Sentinel — Temporal worker entry point.
//
// This binary:
//   1. Connects to the Temporal server.
//   2. Registers the IncidentWorkflow and all activities.
//   3. Starts a worker that polls the "sentinel-incidents" task queue.
//   4. Blocks until SIGTERM/SIGINT, then shuts down gracefully.
//
// Configuration is via environment variables (see .env.example).
// A flag set is also provided for convenience during local development.

package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/devops-sentinel/temporal-worker/activity"
	"github.com/devops-sentinel/temporal-worker/workflow"
)

const (
	// TaskQueue is the name of the Temporal task queue this worker polls.
	// All workflows and activities in this service must use this queue name.
	TaskQueue = "sentinel-incidents"

	// Namespace is the Temporal namespace. "default" is always available
	// in a self-hosted Temporal server with no extra configuration.
	Namespace = "default"
)

func main() {
	setupLogging()

	// ---------------------------------------------------------------------------
	// Configuration
	// Flags override environment variables for convenient local dev overrides.
	// ---------------------------------------------------------------------------
	temporalAddr    := flag.String("temporal-addr",    getenv("TEMPORAL_ADDR",        "localhost:7233"),        "Temporal frontend address (host:port)")
	lokiURL         := flag.String("loki-url",         getenv("LOKI_URL",             "http://localhost:3100"), "Loki HTTP endpoint")
	prometheusURL   := flag.String("prometheus-url",   getenv("PROMETHEUS_URL",       "http://localhost:9090"), "Prometheus HTTP endpoint")
	slackWebhook    := flag.String("slack-webhook",    getenv("SLACK_WEBHOOK_URL",    ""),                     "Slack incoming webhook URL")
	waToken         := flag.String("wa-token",         getenv("WHATSAPP_ACCESS_TOKEN",""),                     "WhatsApp Cloud API access token")
	waPhoneID       := flag.String("wa-phone-id",      getenv("WHATSAPP_PHONE_NUMBER_ID", ""),                 "WhatsApp FROM phone number ID")
	waRecipient     := flag.String("wa-recipient",     getenv("WHATSAPP_RECIPIENT_NUMBER", ""),                "WhatsApp TO phone number (E.164)")
	waTemplate      := flag.String("wa-template",      getenv("WHATSAPP_TEMPLATE_NAME",   "sentinel_alert"),   "WhatsApp template name")
	waTemplateLang  := flag.String("wa-template-lang", getenv("WHATSAPP_TEMPLATE_LANG",   "en_US"),            "WhatsApp template language code")
	flag.Parse()

	// ---------------------------------------------------------------------------
	// Temporal client
	// ---------------------------------------------------------------------------
	slog.Info("connecting to Temporal", "address", *temporalAddr)

	c, err := client.Dial(client.Options{
		HostPort:  *temporalAddr,
		Namespace: Namespace,
	})
	if err != nil {
		slog.Error("failed to create Temporal client", "error", err.Error())
		os.Exit(1)
	}
	defer c.Close()

	slog.Info("Temporal client connected")

	// ---------------------------------------------------------------------------
	// Activity dependencies
	// Activities are plain structs; their dependencies are injected here so
	// the workflow code never has to deal with configuration.
	// ---------------------------------------------------------------------------
	lokiAct := &activity.LokiActivity{BaseURL: *lokiURL}

	promAct := &activity.PrometheusActivity{BaseURL: *prometheusURL}

	notifyAct := &activity.NotifyActivity{
		SlackWebhookURL:       *slackWebhook,
		WhatsAppAccessToken:   *waToken,
		WhatsAppPhoneNumberID: *waPhoneID,
		WhatsAppRecipient:     *waRecipient,
		WhatsAppTemplateName:  *waTemplate,
		WhatsAppTemplateLang:  *waTemplateLang,
	}

	// ---------------------------------------------------------------------------
	// Worker
	// ---------------------------------------------------------------------------
	w := worker.New(c, TaskQueue, worker.Options{})

	// Register the workflow. The Temporal SDK uses the function name as the
	// workflow type name by default. We use a named function to make the type
	// name predictable and refactor-safe.
	w.RegisterWorkflow(workflow.IncidentWorkflow)

	// Register activities as methods on their structs. This is the recommended
	// pattern when activities share dependencies (e.g. a base URL or HTTP client).
	w.RegisterActivity(lokiAct)
	w.RegisterActivity(promAct)
	w.RegisterActivity(notifyAct)

	// ---------------------------------------------------------------------------
	// Start worker and wait for shutdown signal
	// ---------------------------------------------------------------------------
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	slog.Info("starting worker", "taskQueue", TaskQueue, "namespace", Namespace)

	if err := w.Start(); err != nil {
		slog.Error("failed to start worker", "error", err.Error())
		os.Exit(1)
	}

	<-stop
	slog.Info("received shutdown signal, stopping worker")
	w.Stop()
	slog.Info("worker stopped")
}

// setupLogging configures slog to emit structured JSON to stdout.
// This matches the log schema used by the backend in Step 3.
func setupLogging() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

// getenv returns the value of an environment variable or a default value.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
