package evidence

import (
	"encoding/json"
	"testing"
)

func completionCiting(command string) Receipt {
	args, _ := json.Marshal(map[string]any{
		"step":   "wire it up",
		"result": "the switch reaches the gate",
		"evidence": []map[string]any{
			{"kind": "verification", "summary": "the suite passed", "command": command},
		},
	})
	return ReceiptFromToolCall("complete_step", args, true, ToolFacts{})
}

// The case the static table cannot answer: a project's own runner. The model
// names it, the host confirms it ran and passed, and the two together are what
// the table alone could not produce.
func TestCitedCheckCountsWhenTheCommandRanAfterTheWrite(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}})
	writer := 0
	l.Record(Receipt{ToolName: "bash", Command: "./scripts/ci.sh", Success: true})
	l.Record(completionCiting("./scripts/ci.sh"))

	if !l.HasCorroboratedCitedCheckAfter(writer) {
		t.Fatal("a cited check that really ran after the write did not count")
	}
	// The table's own answer is unchanged: this is added evidence, not a
	// reclassification of the command.
	if CommandRunsVerification("./scripts/ci.sh") {
		t.Fatal("precondition: the table should not recognise this command")
	}
}

// A citation is a claim about what ran, so a command with no successful receipt
// after the write proves nothing — this is the whole reason the gate can accept
// the model's naming at all.
func TestCitedCheckIsIgnoredWhenTheCommandNeverRan(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}})
	l.Record(completionCiting("./scripts/ci.sh"))

	if l.HasCorroboratedCitedCheckAfter(0) {
		t.Fatal("a citation with no matching receipt was accepted")
	}
}

// Running the check and then changing the code again leaves the change
// unverified: the citation is anchored to the write it followed, not to any
// write ever made.
func TestCitedCheckDoesNotReachPastALaterWrite(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "bash", Command: "./scripts/ci.sh", Success: true})
	l.Record(completionCiting("./scripts/ci.sh"))
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}})
	laterWriter := 2

	if l.HasCorroboratedCitedCheckAfter(laterWriter) {
		t.Fatal("a check from before the latest write was counted for it")
	}
}

// A failing check cannot be cited into a pass: HasSuccessfulCommandAfter is the
// host's reading of the exit status, and nothing the model writes reaches it.
func TestCitedCheckDoesNotCountWhenTheCommandFailed(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}})
	l.Record(Receipt{ToolName: "bash", Command: "./scripts/ci.sh", Success: false})
	l.Record(completionCiting("./scripts/ci.sh"))

	if l.HasCorroboratedCitedCheckAfter(0) {
		t.Fatal("a failing command was accepted because a completion cited it")
	}
}

// Only verification citations name a check; a completion backed by files or a
// manual note says nothing about a command having run.
func TestOnlyVerificationCitationsNameACheck(t *testing.T) {
	args, _ := json.Marshal(map[string]any{
		"step":   "look at it",
		"result": "read the file",
		"evidence": []map[string]any{
			{"kind": "files", "summary": "read it", "paths": []string{"a.go"}},
			{"kind": "manual", "summary": "looked right"},
		},
	})
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}})
	l.Record(Receipt{ToolName: "bash", Command: "./scripts/ci.sh", Success: true})
	l.Record(ReceiptFromToolCall("complete_step", args, true, ToolFacts{}))

	if l.HasCorroboratedCitedCheckAfter(0) {
		t.Fatal("a completion citing no verification was read as naming a check")
	}
}
