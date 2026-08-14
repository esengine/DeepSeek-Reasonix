package acp

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptStopReason(t *testing.T) {
	tests := []struct {
		name           string
		runErr         error
		cancelled      bool
		recoveryPaused bool
		wantStop       StopReason
		wantError      bool
	}{
		{name: "completed", wantStop: StopEndTurn},
		{name: "cancelled after graceful stop", cancelled: true, wantStop: StopCancelled},
		{name: "cancelled", runErr: errors.New("stopped"), cancelled: true, wantStop: StopCancelled},
		{name: "recovery paused", runErr: errors.New("paused"), recoveryPaused: true, wantStop: StopEndTurn},
		{name: "failed", runErr: errors.New("provider failed"), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStop, gotErr := promptStopReason(tt.runErr, tt.cancelled, tt.recoveryPaused, "session-1")
			if gotStop != tt.wantStop {
				t.Errorf("stopReason = %q, want %q", gotStop, tt.wantStop)
			}
			if (gotErr != nil) != tt.wantError {
				t.Errorf("error = %v, wantError %v", gotErr, tt.wantError)
			}
		})
	}
}

func TestPromptStopReasonRedactsAndClipsFailure(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz"
	const opaqueSecret = "relayKeyAbcdefghijkl"
	const maskedSuffix = "ae54"
	runErr := errors.New("provider failed: Authorization: Bearer " + secret + " credential " + opaqueSecret + " rejected token ****" + maskedSuffix + "\ndetails=" + strings.Repeat("x", 3_000))
	stop, err := promptStopReason(runErr, false, false, "session-1")
	if stop != "" {
		t.Errorf("stopReason = %q, want empty error result", stop)
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != ErrInternal {
		t.Fatalf("error = %#v, want ErrInternal RPCError", err)
	}
	if !strings.HasPrefix(rpcErr.Message, "session/prompt: provider failed:") {
		t.Errorf("error message = %q, want underlying cause", rpcErr.Message)
	}
	if strings.Contains(rpcErr.Message, secret) || strings.Contains(rpcErr.Message, opaqueSecret) || strings.Contains(rpcErr.Message, maskedSuffix) {
		t.Errorf("error message leaked credential: %q", rpcErr.Message)
	}
	if len(rpcErr.Message) > len("session/prompt: ")+2_048 {
		t.Errorf("error message length = %d, want at most %d", len(rpcErr.Message), len("session/prompt: ")+2_048)
	}
}
