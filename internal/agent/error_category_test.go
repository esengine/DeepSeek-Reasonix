package agent

import (
	"testing"

	"reasonix/internal/provider"
)

func TestNormalizedFailureRequiresConsecutiveBatches(t *testing.T) {
	var loop turnLoopState
	call := []provider.ToolCall{{Name: "bash"}}
	failure := []toolOutcome{{errMsg: "exit status 255"}}
	if consecutiveNormalizedFailure(call, failure, &loop) {
		t.Fatal("first failure must not trip")
	}
	if consecutiveNormalizedFailure(call, []toolOutcome{{}}, &loop) {
		t.Fatal("success must reset the category streak")
	}
	if consecutiveNormalizedFailure(call, failure, &loop) {
		t.Fatal("failure after success must start a new streak")
	}
}

func TestNormalizedFailureChecksEveryBatchOutcome(t *testing.T) {
	var loop turnLoopState
	calls := []provider.ToolCall{{Name: "read_file"}, {Name: "bash"}}
	outcomes := []toolOutcome{{}, {errMsg: "exit code: 1"}}
	if consecutiveNormalizedFailure(calls, outcomes, &loop) {
		t.Fatal("first batch failure must not trip")
	}
	if !consecutiveNormalizedFailure(calls, outcomes, &loop) {
		t.Fatal("repeated failure in the second batch item must trip")
	}
}
