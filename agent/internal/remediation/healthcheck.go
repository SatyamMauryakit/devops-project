// internal/remediation/healthcheck.go
// Post-action health verifier with automatic rollback.
//
// After an action is applied, the health checker polls the backend /health
// endpoint every 10 seconds for up to 3 minutes. If health does not return
// HTTP 200 with "status":"ok" within that window, it triggers an automatic
// rollback and notifies via the provided notification function.

package remediation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	defaultPollInterval = 10 * time.Second
	defaultTimeout      = 3 * time.Minute
)

// NotifyFunc is called with a message when auto-rollback occurs.
// In production this calls the notify activity or posts directly to Slack.
type NotifyFunc func(ctx context.Context, title, message string)

// HealthChecker verifies recovery after a remediation action.
type HealthChecker struct {
	// HealthURL is the full URL of the backend /health endpoint.
	// Example: "http://backend:3000/health"
	HealthURL    string
	PollInterval time.Duration
	Timeout      time.Duration
	HTTPClient   *http.Client
}

// NewHealthChecker creates a HealthChecker with sensible defaults.
func NewHealthChecker(healthURL string) *HealthChecker {
	return &HealthChecker{
		HealthURL:    healthURL,
		PollInterval: defaultPollInterval,
		Timeout:      defaultTimeout,
		HTTPClient:   &http.Client{Timeout: 5 * time.Second},
	}
}

// VerifyRecovery polls the health endpoint until it returns healthy or the
// timeout expires. If recovery is not confirmed, it calls rollback and notify.
//
// Returns true if the service recovered, false if auto-rollback was triggered.
func (hc *HealthChecker) VerifyRecovery(
	ctx context.Context,
	ex *Executor,
	state CapturedState,
	workflowID string,
	notify NotifyFunc,
) bool {
	deadline := time.Now().Add(hc.Timeout)

	slog.Info("healthcheck: waiting for recovery",
		"deployment", state.DeploymentName,
		"timeout", hc.Timeout.String(),
		"healthURL", hc.HealthURL,
	)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			slog.Warn("healthcheck: context cancelled", "deployment", state.DeploymentName)
			return false
		case <-time.After(hc.PollInterval):
		}

		if hc.isHealthy(ctx) {
			slog.Info("healthcheck: service recovered",
				"deployment", state.DeploymentName,
				"elapsed", time.Since(state.CapturedAt).Round(time.Second).String(),
			)
			return true
		}

		slog.Info("healthcheck: service not yet healthy, continuing to poll",
			"deployment", state.DeploymentName,
			"remaining", time.Until(deadline).Round(time.Second).String(),
		)
	}

	// Timeout reached — service did not recover. Trigger auto-rollback.
	slog.Error("healthcheck: service did not recover within timeout — rolling back",
		"deployment", state.DeploymentName,
		"workflowID", workflowID,
	)

	rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ex.Rollback(rollbackCtx, state); err != nil {
		msg := fmt.Sprintf("Auto-rollback of %s FAILED: %s. Manual intervention required.", state.DeploymentName, err.Error())
		slog.Error("healthcheck: auto-rollback failed", "error", err.Error())
		if notify != nil {
			notify(ctx, "[AUTO-ROLLBACK FAILED] "+state.DeploymentName, msg)
		}
		return false
	}

	msg := fmt.Sprintf(
		"Service %s did not recover after %s. Automatic rollback applied.\n\nRollback plan: %s",
		state.DeploymentName,
		hc.Timeout.String(),
		buildRollbackPlan(state.DeploymentName, state),
	)

	slog.Warn("healthcheck: auto-rollback applied", "deployment", state.DeploymentName)

	if notify != nil {
		notify(ctx,
			"[AUTO-ROLLBACK] "+state.DeploymentName+" did not recover",
			msg,
		)
	}

	return false
}

// isHealthy calls the health endpoint and returns true if the service reports
// status "ok". Returns false on any error or non-200 response.
func (hc *HealthChecker) isHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.HealthURL, nil)
	if err != nil {
		return false
	}

	resp, err := hc.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	if err != nil {
		return false
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false
	}

	return result.Status == "ok"
}
