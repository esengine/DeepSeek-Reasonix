package control

import "testing"

// A frontend validates a posture name through ParseToolApprovalMode rather than
// listing the postures itself. The HTTP face once listed three of the four and
// answered 400 to dontAsk, so the aliases and the rejection both matter here.
func TestParseToolApprovalMode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"ask", ToolApprovalAsk, true},
		{" Auto ", ToolApprovalAuto, true},
		{"allow", ToolApprovalAuto, true},
		{"dontAsk", ToolApprovalDontAsk, true},
		{"dont-ask", ToolApprovalDontAsk, true},
		{"deny", ToolApprovalDontAsk, true},
		{"YOLO", ToolApprovalYolo, true},
		{"bypass", ToolApprovalYolo, true},
		{"readOnly", ToolApprovalReadOnly, true},
		{"read-only", ToolApprovalReadOnly, true},
		{"surprise", "", false},
		{"", "", false},
	} {
		got, ok := ParseToolApprovalMode(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseToolApprovalMode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
