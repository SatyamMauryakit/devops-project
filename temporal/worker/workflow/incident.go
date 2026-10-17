// workflow/incident.go
// IncidentWorkflow — triggered when Prometheus fires an alert.
//
// ============================================================================
// WORKFLOW DETERMINISM — READ THIS BEFORE MODIFYING THIS FILE
// ============================================================================
//
// Temporal replays a workflow's history to rebuild its state after a worker
// restart or crash. Replay means the workflow function runs again from the
// beginning, but activity calls are short-circuited using recorded results.
//
// For replay to produce identical results, workflow code MUST be deterministic:
// the same sequence of SDK calls must happen on every execution of the function.
//
// NEVER do these things inside a workflow function:
//
//   1. Call time.Now() or time.Sleep().
//      Use workflow.Now(ctx) and workflow.Sleep(ctx, d) instead.
//      Reason: time.Now() returns a different value on replay, breaking history.
//
//   2. Generate random numbers with math/rand or crypto/rand.
//      Use workflow.SideEffect(ctx, f) to record the value on first execution
//      and replay the recorded value on subsequent executions.
//
//   3. Make HTTP calls, read files, or access databases.
//      All I/O belongs in activities. Activities are retried independently
//      and their results are persisted in the workflow history.
//
//   4. Use goroutines directly (go func()).
//      Use workflow.Go(ctx, f) so the Temporal scheduler controls concurrency.
//
//   5. Use global variables or package-level state that changes over time.
//      Workflow code must be a pure function of its inputs and the SDK state.
//
//   6. Use select {} with real channels.
//      Use workflow.GetSignalChannel(ctx, name).Receive(ctx, &v) instead.
//
//   7. Import packages that have side effects on import (e.g. init() that
//      starts goroutines or reads env vars).
//
// Everything that is not a pure computation or SDK call belongs in an activity.
// ============================================================================

package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/devops-sentinel/temporal-worker/activity"
)

const (
	// SignalDecision is the name of the signal that carries the human decision.
	// Acceptable values: "approve", "reject", "ignore".
	SignalDecision = "decision"

	// DecisionApprove means the human approves the proposed remediation.
	DecisionApprove = "approve"
	// DecisionReject means the human rejects the proposed remediation.
	DecisionReject = "reject"
	// DecisionIgnore means the human has reviewed it and considers it a false positive.
	DecisionIgnore = "ignore"

	// FirstDecisionTimeout is how long the workflow waits for the first response.
	FirstDecisionTimeout = 30 * time.Minute

	// ReminderDecisionTimeout is how long the workflow waits after sending a reminder.
	ReminderDecisionTimeout = 15 * time.Minute

	TaskQueue = "sentinel-incidents"
)

// IncidentInput holds the parameters passed when the workflow is started.
// It is populated by the Alertmanager webhook or the notifier service.
type IncidentInput struct {
	// AlertName is the Prometheus alert name, e.g. "BackendDown".
	AlertName string
	// Severity is the alert severity label, e.g. "critical" or "warning".
	Severity string
	// Summary is the alert summary annotation.
	Summary string
	// Description is the alert description annotation.
	Description string
	// StartsAt is when the alert started firing (RFC3339 string).
	StartsAt string
}

// IncidentResult is what the workflow returns when it completes.
type IncidentResult struct {
	// Decision is the final human decision: approve, reject, ignore, or timeout.
	Decision string
	// ActionsApplied describes what was done (empty if rejected/ignored/timeout).
	ActionsApplied string
	// EvidenceSummary is a short summary of logs and metrics gathered.
	EvidenceSummary string
}

// IncidentWorkflow is the main workflow function.
// It is registered in main.go as workflow.IncidentWorkflow.
//
// Lifecycle:
//  1. Fetch evidence (logs from Loki, metrics from Prometheus) in parallel.
//  2. Send a notification to Slack and WhatsApp with the evidence summary
//     and a request for a human decision signal.
//  3. Wait up to FirstDecisionTimeout for the "decision" signal.
//  4. If no signal arrives, send a reminder and wait ReminderDecisionTimeout.
//  5. If still no signal, escalate (log at error, record outcome as "timeout").
//  6. If signal arrives:
//     - "approve":  record approval, note actions to be applied (Step 8 applies them).
//     - "reject":   record rejection, close workflow.
//     - "ignore":   record false positive, close workflow.
//
// NOTE: No remediation is applied in this step. That is Step 8 (AI agent).
// This workflow only gathers evidence, notifies, and records the decision.
func IncidentWorkflow(ctx workflow.Context, input IncidentInput) (IncidentResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("IncidentWorkflow started",
		"alertName", input.AlertName,
		"severity", input.Severity,
	)

	// -------------------------------------------------------------------------
	// Activity options
	// All activities share the same retry policy. The StartToCloseTimeout is
	// the maximum time a single attempt may run. ScheduleToCloseTimeout caps
	// the total time including all retries.
	// -------------------------------------------------------------------------
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		ScheduleToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
			// Do not retry on bad input errors — they will never succeed.
			NonRetryableErrorTypes: []string{"BadRequestError"},
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	// -------------------------------------------------------------------------
	// Step 1: Fetch evidence in parallel using workflow.Go
	// workflow.Go is the deterministic equivalent of "go func()".
	// -------------------------------------------------------------------------
	var (
		logsResult    activity.FetchLogsResult
		metricsResult activity.FetchMetricsResult
		logsErr       error
		metricsErr    error
	)

	wg := workflow.NewWaitGroup(ctx)
	wg.Add(2)

	workflow.Go(ctx, func(gCtx workflow.Context) {
		defer wg.Done()
		logsErr = workflow.ExecuteActivity(
			workflow.WithActivityOptions(gCtx, actOpts),
			activity.LokiActivityName,
			activity.FetchLogsInput{
				Service:  "devops-sentinel-backend",
				Duration: "15m",
				Level:    "error",
			},
		).Get(gCtx, &logsResult)
	})

	workflow.Go(ctx, func(gCtx workflow.Context) {
		defer wg.Done()
		metricsErr = workflow.ExecuteActivity(
			workflow.WithActivityOptions(gCtx, actOpts),
			activity.PrometheusActivityName,
			activity.FetchMetricsInput{
				AlertName: input.AlertName,
			},
		).Get(gCtx, &metricsResult)
	})

	wg.Wait(ctx)

	// Errors from evidence gathering are non-fatal: we still notify.
	// Log them and include a note in the evidence summary.
	evidenceSummary := buildEvidenceSummary(input, logsResult, metricsResult, logsErr, metricsErr)
	logger.Info("evidence gathered", "summary", evidenceSummary)

	// -------------------------------------------------------------------------
	// Step 2: Send initial notification
	// -------------------------------------------------------------------------
	notifyInput := activity.SendNotificationInput{
		Title:           fmt.Sprintf("[%s] %s — action required", input.Severity, input.AlertName),
		Body:            fmt.Sprintf("*%s*\n%s\n\n*Evidence:*\n%s\n\nReply with signal 'decision': approve | reject | ignore", input.Summary, input.Description, evidenceSummary),
		WhatsAppText:    fmt.Sprintf("[%s] %s: %s. Send signal 'decision' to approve, reject, or ignore.", input.Severity, input.AlertName, input.Summary),
		Color:           colorForSeverity(input.Severity),
	}

	var notifyResult activity.SendNotificationResult
	_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, notifyInput).Get(actCtx, &notifyResult)

	// -------------------------------------------------------------------------
	// Step 3: Wait for human decision signal with timeout
	// workflow.GetSignalChannel + workflow.NewTimer is the correct deterministic
	// pattern. Do NOT use time.After() or time.NewTimer().
	// -------------------------------------------------------------------------
	decisionCh := workflow.GetSignalChannel(ctx, SignalDecision)

	var decision string

	// First wait window.
	firstTimer := workflow.NewTimer(ctx, FirstDecisionTimeout)
	selector := workflow.NewSelector(ctx)

	selector.AddReceive(decisionCh, func(ch workflow.ReceiveChannel, more bool) {
		ch.Receive(ctx, &decision)
	})
	selector.AddFuture(firstTimer, func(_ workflow.Future) {
		// Timer fired — no decision received in time.
		decision = ""
	})

	selector.Select(ctx)

	if decision == "" {
		// -----------------------------------------------------------------------
		// Step 4: Send reminder after first timeout
		// -----------------------------------------------------------------------
		logger.Warn("no decision received, sending reminder",
			"alertName", input.AlertName,
			"waitedMinutes", int(FirstDecisionTimeout.Minutes()),
		)

		reminderInput := activity.SendNotificationInput{
			Title:        fmt.Sprintf("[REMINDER] %s — still waiting for decision", input.AlertName),
			Body:         fmt.Sprintf("No response received after %d minutes.\n\nPlease send signal 'decision': approve | reject | ignore\n\n*Original alert:* %s", int(FirstDecisionTimeout.Minutes()), input.Summary),
			WhatsAppText: fmt.Sprintf("[REMINDER] %s still awaiting decision after %d min.", input.AlertName, int(FirstDecisionTimeout.Minutes())),
			Color:        "warning",
		}
		_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, reminderInput).Get(actCtx, nil)

		// Second wait window.
		secondTimer := workflow.NewTimer(ctx, ReminderDecisionTimeout)
		selector2 := workflow.NewSelector(ctx)

		selector2.AddReceive(decisionCh, func(ch workflow.ReceiveChannel, more bool) {
			ch.Receive(ctx, &decision)
		})
		selector2.AddFuture(secondTimer, func(_ workflow.Future) {
			decision = ""
		})

		selector2.Select(ctx)
	}

	// -------------------------------------------------------------------------
	// Step 5: Escalate if still no decision
	// -------------------------------------------------------------------------
	if decision == "" {
		logger.Error("escalating — no human decision received after reminder",
			"alertName", input.AlertName,
			"totalWaitMinutes", int((FirstDecisionTimeout + ReminderDecisionTimeout).Minutes()),
		)

		escalationInput := activity.SendNotificationInput{
			Title:        fmt.Sprintf("[ESCALATION] %s — no decision after %d minutes", input.AlertName, int((FirstDecisionTimeout+ReminderDecisionTimeout).Minutes())),
			Body:         fmt.Sprintf("No human decision was received. The incident is being recorded as timed out.\n\n*Alert:* %s\n*Evidence:* %s", input.Summary, evidenceSummary),
			WhatsAppText: fmt.Sprintf("[ESCALATION] %s: no decision received. Incident timed out.", input.AlertName),
			Color:        "danger",
		}
		_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, escalationInput).Get(actCtx, nil)

		return IncidentResult{
			Decision:        "timeout",
			EvidenceSummary: evidenceSummary,
		}, nil
	}

	// -------------------------------------------------------------------------
	// Step 6: Process the human decision
	// -------------------------------------------------------------------------
	logger.Info("decision received", "decision", decision, "alertName", input.AlertName)

	var actionsApplied string

	switch decision {
	case DecisionApprove:
		// Remediation is applied in Step 8 (AI agent). For now we record that
		// approval was given and log the reasoning and rollback plan.
		actionsApplied = fmt.Sprintf(
			"Approved by human operator. Rollback plan: scale deployment/backend to previous replica count. Remediation will be applied by the AI agent (Step 8).",
		)

		approvalNotice := activity.SendNotificationInput{
			Title:        fmt.Sprintf("[APPROVED] %s — remediation will proceed", input.AlertName),
			Body:         fmt.Sprintf("Human approved remediation.\n\n*Evidence:* %s\n*Actions:* %s", evidenceSummary, actionsApplied),
			WhatsAppText: fmt.Sprintf("[APPROVED] %s: remediation proceeding.", input.AlertName),
			Color:        "good",
		}
		_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, approvalNotice).Get(actCtx, nil)

	case DecisionReject:
		actionsApplied = "Remediation rejected by human operator. No changes applied."

		rejectNotice := activity.SendNotificationInput{
			Title:        fmt.Sprintf("[REJECTED] %s — no action taken", input.AlertName),
			Body:         fmt.Sprintf("Human rejected remediation. No changes applied.\n\n*Evidence:* %s", evidenceSummary),
			WhatsAppText: fmt.Sprintf("[REJECTED] %s: no action taken.", input.AlertName),
			Color:        "warning",
		}
		_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, rejectNotice).Get(actCtx, nil)

	case DecisionIgnore:
		actionsApplied = "Marked as false positive by human operator."

		ignoreNotice := activity.SendNotificationInput{
			Title:        fmt.Sprintf("[IGNORED] %s — false positive", input.AlertName),
			Body:         fmt.Sprintf("Human marked this alert as a false positive. No changes applied."),
			WhatsAppText: fmt.Sprintf("[IGNORED] %s: false positive.", input.AlertName),
			Color:        "good",
		}
		_ = workflow.ExecuteActivity(actCtx, activity.NotifyActivityName, ignoreNotice).Get(actCtx, nil)

	default:
		logger.Warn("unknown decision value received", "decision", decision)
		actionsApplied = fmt.Sprintf("Unknown decision value '%s' received. No action taken.", decision)
	}

	logger.Info("IncidentWorkflow completed",
		"alertName", input.AlertName,
		"decision", decision,
	)

	return IncidentResult{
		Decision:        decision,
		ActionsApplied:  actionsApplied,
		EvidenceSummary: evidenceSummary,
	}, nil
}

// buildEvidenceSummary constructs a human-readable summary from activity results.
// This is pure computation — no I/O — so it is safe inside a workflow.
func buildEvidenceSummary(
	input IncidentInput,
	logs activity.FetchLogsResult,
	metrics activity.FetchMetricsResult,
	logsErr, metricsErr error,
) string {
	summary := fmt.Sprintf("Alert: %s (%s)\nStarted: %s\n\n", input.AlertName, input.Severity, input.StartsAt)

	if logsErr != nil {
		summary += fmt.Sprintf("Logs: unavailable (%s)\n", logsErr.Error())
	} else {
		summary += fmt.Sprintf("Recent errors (%d lines):\n%s\n", logs.LineCount, logs.Excerpt)
	}

	if metricsErr != nil {
		summary += fmt.Sprintf("Metrics: unavailable (%s)\n", metricsErr.Error())
	} else {
		summary += fmt.Sprintf("Metrics snapshot:\n%s\n", metrics.Summary)
	}

	return summary
}

// colorForSeverity maps a Prometheus severity label to a Slack color name.
func colorForSeverity(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	default:
		return "good"
	}
}
