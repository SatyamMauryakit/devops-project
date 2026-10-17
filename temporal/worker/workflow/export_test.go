// export_test.go
// Exports unexported functions for use in tests.
// This file is only compiled when running tests.

package workflow

import "github.com/devops-sentinel/temporal-worker/activity"

// BuildEvidenceSummaryForTest exposes buildEvidenceSummary for unit tests.
func BuildEvidenceSummaryForTest(
	input IncidentInput,
	logs activity.FetchLogsResult,
	metrics activity.FetchMetricsResult,
	logsErr, metricsErr error,
) string {
	return buildEvidenceSummary(input, logs, metrics, logsErr, metricsErr)
}

// ColorForSeverityForTest exposes colorForSeverity for unit tests.
func ColorForSeverityForTest(severity string) string {
	return colorForSeverity(severity)
}
