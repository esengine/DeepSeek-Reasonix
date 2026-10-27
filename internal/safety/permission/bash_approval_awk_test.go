package permission

import "testing"

func TestAwkProgramInTheCallNeedsTheInlineCodeApproval(t *testing.T) {
	if got := indirectExecutionBlocker([]string{"awk", `BEGIN{system("id")}`}); got != BashApprovalBlockerInlineCode {
		t.Errorf("awk with its program in the call: blocker %v, want inline code", got)
	}
	if got := indirectExecutionBlocker([]string{"awk", "-f", "report.awk", "data.txt"}); got != BashApprovalBlockerNone {
		t.Errorf("awk reading its program from a file: blocker %v, want none", got)
	}
}
