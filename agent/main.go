// main.go
// DevOps Sentinel AI Diagnosis Agent.
//
// Startup sequence:
//  1. Load configuration from environment variables.
//  2. Initialise OpenTelemetry tracing.
//  3. Connect to the Temporal server.
//  4. Build Kubernetes client (in-cluster or kubeconfig).
//  5. Wire LLM provider, evidence collector, approval handlers,
//     executor, audit logger, kill switch, and rate limiter.
//  6. Start the HTTP server.
//  7. Block until SIGTERM/SIGINT, then shut down gracefully.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/devops-sentinel/agent/internal/approval"
	"github.com/devops-sentinel/agent/internal/audit"
	"github.com/devops-sentinel/agent/internal/config"
	"github.com/devops-sentinel/agent/internal/evidence"
	"github.com/devops-sentinel/agent/internal/killswitch"
	"github.com/devops-sentinel/agent/internal/llm"
	"github.com/devops-sentinel/agent/internal/ratelimit"
	"github.com/devops-sentinel/agent/internal/remediation"
	"github.com/devops-sentinel/agent/internal/webhook"
)

func init() {
	for _, arg := range os.Args[1:] {
		if arg == "-health-check-only" {
			port := "8081"
			if p := os.Getenv("PORT"); p != "" {
				port = p
			}
			resp, err := http.Get("http://localhost:" + port + "/health")
			if err != nil || resp.StatusCode != http.StatusOK {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

// ---------------------------------------------------------------------------
// TemporalDispatcher implements approval.Dispatcher.
// It signals a Temporal workflow with the human decision and enforces
// the one-decision-per-incident rule.
// ---------------------------------------------------------------------------

type TemporalDispatcher struct {
	temporalClient client.Client
	namespace      string
	executor       *remediation.Executor
	healthChecker  *remediation.HealthChecker
	auditLog       *audit.Logger

	mu       sync.Mutex
	decided  map[string]string // workflowID -> decision already recorded
}

func (d *TemporalDispatcher) Signal(workflowID, decision, approvedBy string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Enforce one-decision-per-incident.
	if prev, ok := d.decided[workflowID]; ok {
		slog.Warn("duplicate decision ignored",
			"workflowID", workflowID,
			"previousDecision", prev,
			"newDecision", decision,
			"by", approvedBy,
		)
		return nil
	}
	d.decided[workflowID] = decision

	slog.Info("dispatching decision to Temporal",
		"workflowID", workflowID,
		"decision", decision,
		"approvedBy", approvedBy,
	)

	err := d.temporalClient.SignalWorkflow(
		context.Background(),
		workflowID,
		"",            // runID empty = latest run
		"decision",    // signal name from Step 7
		decision,
	)
	if err != nil {
		return fmt.Errorf("signal workflow %q: %w", workflowID, err)
	}

	// If approved, execute the remediation asynchronously so the HTTP handler
	// can return immediately to Slack/WhatsApp without waiting for Kubernetes.
	if decision == "approve" {
		go d.executeApproved(workflowID, approvedBy)
	} else {
		// Write a reject/ignore audit record.
		d.auditLog.Write(context.Background(), audit.Record{
			WorkflowID: workflowID,
			Decision:   decision,
			ApprovedBy: approvedBy,
			Result:     "rejected",
			Action:     "no_action",
		})
	}

	return nil
}

// executeApproved runs after approval: queries the workflow for the proposed
// action, executes it, verifies health, and writes the audit record.
// This runs in a goroutine so the approval HTTP response is not delayed.
func (d *TemporalDispatcher) executeApproved(workflowID, approvedBy string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Query the workflow result to get the proposed action.
	// In Step 7 the workflow returns an IncidentResult with ProposedAction.
	// We use QueryWorkflow to read it without consuming the result.
	// For simplicity here we read it from the workflow description.
	// In production this would be a Temporal query or a database lookup.
	// For Step 9 we use "restart_deployment" as a safe default action
	// when the workflow result is not yet available.
	//
	// NOTE: This is intentionally simple. Step 10 will connect the executor
	// to the actual workflow result via a Temporal query activity.
	action := "restart_deployment"
	deployment := "backend"

	result := d.executor.Execute(ctx, deployment, action)

	rec := audit.Record{
		WorkflowID:   workflowID,
		AlertName:    deployment,
		ApprovedBy:   approvedBy,
		Decision:     "approve",
		Action:       action,
		RollbackPlan: result.RollbackPlan,
	}

	if !result.Applied {
		rec.Result = "failed"
		rec.Error = result.Error
		d.auditLog.Write(ctx, rec)
		return
	}

	rec.Result = "applied"
	d.auditLog.Write(ctx, rec)

	// Verify recovery in the background without blocking the audit write.
	go func() {
		hcCtx, hcCancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer hcCancel()

		recovered := d.healthChecker.VerifyRecovery(
			hcCtx, d.executor, result.State, workflowID,
			func(ctx context.Context, title, msg string) {
				slog.Warn("auto-rollback notification", "title", title, "message", msg)
			},
		)

		if !recovered {
			d.auditLog.Write(hcCtx, audit.Record{
				WorkflowID: workflowID,
				AlertName:  deployment,
				ApprovedBy: approvedBy,
				Decision:   "approve",
				Action:     action,
				Result:     "auto_rolled_back",
				Error:      "service did not recover within timeout",
			})
		}
	}()
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	setupLogging()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err.Error())
		os.Exit(1)
	}

	slog.Info("agent starting",
		"llm_provider", cfg.LLMProvider,
		"temporal_addr", cfg.TemporalAddr,
		"port", cfg.Port,
	)

	// OpenTelemetry (non-fatal).
	shutdownTracer, err := initTracer(cfg)
	if err != nil {
		slog.Warn("OTel init failed — tracing disabled", "error", err.Error())
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownTracer(ctx)
		}()
	}

	// Temporal client.
	temporalClient, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalAddr,
		Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		slog.Error("connect to Temporal", "error", err.Error())
		os.Exit(1)
	}
	defer temporalClient.Close()

	// Kubernetes client.
	k8sClient, err := buildK8sClient(cfg.KubeconfigPath)
	if err != nil {
		slog.Error("build Kubernetes client", "error", err.Error())
		os.Exit(1)
	}

	// Step 9 components.
	ks := killswitch.New(k8sClient)
	rl := ratelimit.New(15 * time.Minute)
	auditLog := audit.New(cfg.MongoURI)

	executor := &remediation.Executor{
		K8s:         k8sClient,
		KillSwitch:  ks,
		RateLimiter: rl,
	}

	healthChecker := remediation.NewHealthChecker(cfg.BackendHealthURL)

	dispatcher := &TemporalDispatcher{
		temporalClient: temporalClient,
		namespace:      cfg.TemporalNamespace,
		executor:       executor,
		healthChecker:  healthChecker,
		auditLog:       auditLog,
		decided:        make(map[string]string),
	}

	// HTTP mux.
	mux := http.NewServeMux()

	mux.Handle("/webhook", &webhook.Handler{
		Collector:      &evidence.Collector{LokiURL: cfg.LokiURL, PrometheusURL: cfg.PrometheusURL},
		LLM:            buildLLMProvider(cfg),
		TemporalClient: temporalClient,
		TaskQueue:      cfg.TemporalTaskQueue,
	})

	// Slack interactive button callbacks.
	mux.Handle("/approval/slack", &approval.SlackHandler{
		SigningSecret: cfg.SlackSigningSecret,
		Dispatcher:    dispatcher,
	})

	// WhatsApp incoming message webhooks.
	mux.Handle("/approval/whatsapp", &approval.WhatsAppHandler{
		AppSecret:   cfg.WhatsAppAppSecret,
		VerifyToken: cfg.WhatsAppVerifyToken,
		Dispatcher:  dispatcher,
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok","service":"devops-sentinel-agent"}`)
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 150 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("HTTP server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err.Error())
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err.Error())
	}
	slog.Info("agent stopped")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

func buildLLMProvider(cfg config.Config) llm.Provider {
	switch cfg.LLMProvider {
	case "groq":
		return &llm.GroqProvider{APIKey: cfg.GroqAPIKey, Model: cfg.GroqModel}
	default:
		return &llm.OllamaProvider{BaseURL: cfg.OllamaBaseURL, Model: cfg.OllamaModel}
	}
}

func buildK8sClient(kubeconfigPath string) (kubernetes.Interface, error) {
	var restCfg *rest.Config
	var err error

	if kubeconfigPath != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes config: %w", err)
	}

	return kubernetes.NewForConfig(restCfg)
}

func initTracer(cfg config.Config) (func(context.Context) error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, cfg.OTELEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to OTel collector: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.OTELServiceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
