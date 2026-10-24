package tool

import "testing"

func TestBashVerificationPayloadRejectsReplacementCommands(t *testing.T) {
	for _, marker := range []string{"replacement", "__rx_pipestatus_'; exit 0; #", "__rx_pipestatus_0123456789abcdef\n"} {
		if _, err := BashVerificationPayload("false", marker); err == nil {
			t.Fatalf("accepted executable marker %q", marker)
		}
	}
	if got, err := BashVerificationPayload("false", ""); err != nil || got != "false" {
		t.Fatalf("changed unwrapped command: %q %v", got, err)
	}
}
