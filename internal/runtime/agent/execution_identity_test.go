package agent

import "testing"

func TestExecutionIdentityIsConsumedOnceAndNotInherited(t *testing.T) {
	root := beginExecutionContext(t.Context())
	child := beginExecutionContext(root)
	if executionID(root) == "" || executionID(child) == "" || executionID(root) == executionID(child) {
		t.Fatal("nested runs share an execution identity")
	}
	bound := beginExecutionContext(WithRunExecution(root, "persisted-task"))
	if executionID(bound) != "persisted-task" {
		t.Fatal("lost host identity")
	}
	nested := beginExecutionContext(bound)
	if executionID(nested) == executionID(bound) {
		t.Fatal("consumed identity leaked into nested run")
	}
}
