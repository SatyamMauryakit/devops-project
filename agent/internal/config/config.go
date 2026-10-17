// internal/config/config.go
// Loads all agent configuration from environment variables.
// No flags — Kubernetes injects config via env. Local dev uses a .env file
// loaded by the caller before this package is used.

package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the complete runtime configuration for the agent.
// Every field maps to exactly one environment variable.
type Config struct {
	// HTTP server
	Port     string
	LogLevel string

	// Temporal
	TemporalAddr      string
	TemporalNamespace string
	TemporalTaskQueue string

	// Data sources
	LokiURL       string
	PrometheusURL string

	// LLM
	LLMProvider   string // "ollama" | "groq"
	OllamaBaseURL string
	OllamaModel   string
	GroqAPIKey    string
	GroqModel     string

	// OpenTelemetry
	OTELEndpoint    string
	OTELServiceName string

	// Slack fallback (used only if the Temporal worker is unreachable)
	SlackWebhookURL string

	// Step 9: approval webhooks
	SlackSigningSecret   string
	WhatsAppAppSecret    string
	WhatsAppVerifyToken  string

	// Step 9: Kubernetes in-cluster config
	// When empty, the agent uses the in-cluster ServiceAccount token.
	// Set to a kubeconfig path for local development.
	KubeconfigPath string

	// Step 9: MongoDB URI for the audit log
	MongoURI string

	// Step 9: backend health URL for post-action health check
	BackendHealthURL string
}

// Load reads all configuration from the environment.
// Returns an error if any required variable is missing.
func Load() (Config, error) {
	c := Config{
		Port:              getenv("PORT", "8081"),
		LogLevel:          getenv("LOG_LEVEL", "info"),
		TemporalAddr:      getenv("TEMPORAL_ADDR", "localhost:7233"),
		TemporalNamespace: getenv("TEMPORAL_NAMESPACE", "default"),
		TemporalTaskQueue: getenv("TEMPORAL_TASK_QUEUE", "sentinel-incidents"),
		LokiURL:           getenv("LOKI_URL", "http://localhost:3100"),
		PrometheusURL:     getenv("PROMETHEUS_URL", "http://localhost:9090"),
		LLMProvider:       strings.ToLower(getenv("LLM_PROVIDER", "ollama")),
		OllamaBaseURL:     getenv("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaModel:       getenv("OLLAMA_MODEL", "llama3.1:8b"),
		GroqAPIKey:        os.Getenv("GROQ_API_KEY"),
		GroqModel:         getenv("GROQ_MODEL", "llama3-8b-8192"),
		OTELEndpoint:      getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317"),
		OTELServiceName:   getenv("OTEL_SERVICE_NAME", "devops-sentinel-agent"),
		SlackWebhookURL:   os.Getenv("SLACK_WEBHOOK_URL"),

		SlackSigningSecret:  os.Getenv("SLACK_SIGNING_SECRET"),
		WhatsAppAppSecret:   os.Getenv("WHATSAPP_APP_SECRET"),
		WhatsAppVerifyToken: getenv("WHATSAPP_VERIFY_TOKEN", "sentinel-verify"),
		KubeconfigPath:      os.Getenv("KUBECONFIG"),
		MongoURI:            getenv("MONGO_URI", "mongodb://localhost:27017"),
		BackendHealthURL:    getenv("BACKEND_HEALTH_URL", "http://backend:3000/health"),
	}

	// Validate provider-specific required fields.
	switch c.LLMProvider {
	case "ollama":
		// Ollama needs no credentials; base URL has a sane default.
	case "groq":
		if c.GroqAPIKey == "" {
			return Config{}, fmt.Errorf("LLM_PROVIDER=groq requires GROQ_API_KEY to be set")
		}
	default:
		return Config{}, fmt.Errorf("LLM_PROVIDER must be 'ollama' or 'groq', got %q", c.LLMProvider)
	}

	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
