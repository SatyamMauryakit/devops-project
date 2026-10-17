// export_test.go — test-only exports for unexported helpers.
package approval

// ExtractWorkflowIDForTest exposes extractWorkflowID for unit tests.
func ExtractWorkflowIDForTest(actionID string) string {
	return extractWorkflowID(actionID)
}
