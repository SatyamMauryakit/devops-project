// workflow_test.go
// Tests for IncidentWorkflow using the Temporal test framework.
//
// The Temporal test framework (testsuite.WorkflowTestSuite) runs workflows
// in a simulated environment:
//   - No real Temporal server is needed.
//   - Time is controlled by the test: workflow.Sleep and workflow.NewTimer
//     advance instantly unless you call env.Sleep() explicitly.
//   - Activities are replaced with mock functions so no real HTTP calls are made.
//   - Signals can be sent programmatically via env.SignalWorkflow().
//
// Run the tests:
//   cd temporal/worker
//   go test ./... -v -count=1

package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/devops-sentinel/temporal-worker/activity"
	wf "github.com/devops-sentinel/temporal-worker/workflow"
)

// ---------------------------------------------------------------------------
// Test suite setup
// ---------------------------------------------------------------------------

// IncidentWorkflowTestSuite embeds WorkflowTestSuite and TestSuite so the
// Temporal test environment is initialised automatically per-test.
type IncidentWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite

	env *testsuite.TestWorkflowEnvironment
}

func (s *IncidentWorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *IncidentWorkflowTestSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

// Run the test suite.
func TestIncidentWorkflowSuite(t *testing.T) {
	suite.Run(t, new(IncidentWorkflowTestSuite))
}

// ---------------------------------------------------------------------------
// Helper: standard input used by most tests
// ---------------------------------------------------------------------------

func testInput() wf.IncidentInput {
	return wf.IncidentInput{
		AlertName:   "BackendDown",
		Severity:    "critical",
		Summary:     "Backend deployment has zero ready replicas",
		Description: "All backend pods are unready.",
		StartsAt:    time.Now().Format(time.RFC3339),
	}
}

// ---------------------------------------------------------------------------
// Helper: register standard activity mocks that succeed with stub data
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) mockActivitiesOK() {
	s.env.OnActivity(activity.LokiActivityName, mock.Anything, mock.Anything).
		Return(activity.FetchLogsResult{
			LineCount: 3,
			Excerpt:   `{"level":"error","message":"connection refused"}`,
		}, nil)

	s.env.OnActivity(activity.PrometheusActivityName, mock.Anything, mock.Anything).
		Return(activity.FetchMetricsResult{
			Summary:      "ready replicas: 0\ndesired replicas: 2",
			FiringAlerts: 1,
		}, nil)

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true, WhatsAppSent: false}, nil).
		Times(0) // allow any number of notify calls
}

// ---------------------------------------------------------------------------
// Test 1: approve path
// The human sends "approve" immediately after the workflow starts.
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_Approve() {
	s.mockActivitiesOK()

	// Allow any number of notify calls (initial + approval confirmation).
	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	// Send the "approve" signal after a short simulated delay.
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, wf.DecisionApprove)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(wf.DecisionApprove, result.Decision)
	s.NotEmpty(result.ActionsApplied)
	s.NotEmpty(result.EvidenceSummary)
}

// ---------------------------------------------------------------------------
// Test 2: reject path
// The human sends "reject". Workflow completes with no action applied.
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_Reject() {
	s.mockActivitiesOK()

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, wf.DecisionReject)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(wf.DecisionReject, result.Decision)
	s.Contains(result.ActionsApplied, "rejected")
}

// ---------------------------------------------------------------------------
// Test 3: ignore path (false positive)
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_Ignore() {
	s.mockActivitiesOK()

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, wf.DecisionIgnore)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(wf.DecisionIgnore, result.Decision)
	s.Contains(result.ActionsApplied, "false positive")
}

// ---------------------------------------------------------------------------
// Test 4: timeout path — no signal arrives
// The test framework advances time past both timeouts so the workflow
// reaches the escalation branch.
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_Timeout_Escalation() {
	s.mockActivitiesOK()

	// Allow any number of notify calls (initial + reminder + escalation).
	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	// No signal is sent. The test framework will fast-forward all timers.
	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal("timeout", result.Decision)
	s.NotEmpty(result.EvidenceSummary)
}

// ---------------------------------------------------------------------------
// Test 5: approve after first timeout (signal arrives during reminder window)
// Simulates a slow human who reads the reminder and then approves.
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_ApproveAfterReminder() {
	s.mockActivitiesOK()

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	// Signal arrives AFTER the first timeout but BEFORE the reminder timeout.
	// The test framework auto-advances time for timers, so we add a delay that
	// falls between the two timeouts.
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, wf.DecisionApprove)
	}, wf.FirstDecisionTimeout+5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(wf.DecisionApprove, result.Decision)
}

// ---------------------------------------------------------------------------
// Test 6: activity failure is retried and then succeeds
// Simulates a transient Loki failure. The mock returns an error on the first
// call and succeeds on the second, exercising the retry policy path.
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_LokiRetry() {
	// First call fails, second succeeds.
	s.env.OnActivity(activity.LokiActivityName, mock.Anything, mock.Anything).
		Return(activity.FetchLogsResult{}, fmt.Errorf("loki connection refused")).Once()

	s.env.OnActivity(activity.LokiActivityName, mock.Anything, mock.Anything).
		Return(activity.FetchLogsResult{LineCount: 1, Excerpt: "error log line"}, nil).Once()

	s.env.OnActivity(activity.PrometheusActivityName, mock.Anything, mock.Anything).
		Return(activity.FetchMetricsResult{Summary: "ready: 0"}, nil)

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, wf.DecisionApprove)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(wf.DecisionApprove, result.Decision)
}

// ---------------------------------------------------------------------------
// Test 7: unknown decision value — workflow completes without error
// ---------------------------------------------------------------------------

func (s *IncidentWorkflowTestSuite) Test_UnknownDecision() {
	s.mockActivitiesOK()

	s.env.OnActivity(activity.NotifyActivityName, mock.Anything, mock.Anything).
		Return(activity.SendNotificationResult{SlackSent: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(wf.SignalDecision, "banana")
	}, 5*time.Second)

	s.env.ExecuteWorkflow(wf.IncidentWorkflow, testInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result wf.IncidentResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal("banana", result.Decision)
	s.Contains(result.ActionsApplied, "Unknown decision")
}

// ---------------------------------------------------------------------------
// Standalone unit tests for pure helper functions (no Temporal test env needed)
// ---------------------------------------------------------------------------

func TestBuildEvidenceSummary_WithErrors(t *testing.T) {
	input := wf.IncidentInput{
		AlertName: "BackendDown",
		Severity:  "critical",
		Summary:   "Backend is down",
		StartsAt:  "2026-10-17T10:00:00Z",
	}

	result := wf.BuildEvidenceSummaryForTest(
		input,
		activity.FetchLogsResult{},
		activity.FetchMetricsResult{},
		fmt.Errorf("loki unavailable"),
		fmt.Errorf("prometheus unavailable"),
	)

	if result == "" {
		t.Fatal("evidence summary should not be empty")
	}
	if !contains(result, "BackendDown") {
		t.Errorf("expected 'BackendDown' in summary, got: %s", result)
	}
	if !contains(result, "unavailable") {
		t.Errorf("expected 'unavailable' in summary, got: %s", result)
	}
}

func TestColorForSeverity(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"critical", "danger"},
		{"warning", "warning"},
		{"info", "good"},
		{"", "good"},
	}
	for _, tc := range cases {
		got := wf.ColorForSeverityForTest(tc.severity)
		if got != tc.want {
			t.Errorf("severity %q: want %q, got %q", tc.severity, tc.want, got)
		}
	}
}

// contains is a simple substring check for test assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) == 0 ||
		(len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
