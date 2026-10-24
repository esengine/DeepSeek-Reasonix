package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func TestTerminalEventsSurviveProviderDiagnosticFailure(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, status := range []event.TurnStatus{event.TurnFailed, event.TurnInterrupted, event.TurnRecoveryRequired} {
		t.Run(string(status), func(t *testing.T) {
			var logs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			c := &Controller{}
			c.beginProviderDiagnosticTurn("turn")
			// Use the real encoder's failure path, without a production test hook.
			c.recordProviderRequest("turn", provider.RequestObservation{ID: 1, Phase: "request_started",
				StartedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), RemoteAddress: "private-peer"})
			e := event.Event{Kind: event.TurnDone, TurnID: "turn", Status: status, Err: errors.New("fixture failure"), Cancelled: status == event.TurnInterrupted}
			if status == event.TurnRecoveryRequired {
				e.Recovery = &event.RecoveryStatus{State: "recovery_required", Reason: "uncooperative_tool"}
			}
			if _, err := c.providerDiagnosticEvent(e); err == nil {
				t.Fatal("fixture did not fail diagnostic encoding")
			}
			projection := session.Projection{ActiveTools: map[string]string{"tool": "bash"},
				StartedTools: map[string]bool{"tool": true}, Interactions: map[string]string{"approval": "pending"},
				ActiveSteps: map[string]bool{"step": true}}
			events, err := c.terminalSessionEvents(e, projection)
			if err != nil {
				t.Fatalf("optional diagnostic blocked terminal events: %v", err)
			}
			var kinds []string
			for _, ev := range events {
				kinds = append(kinds, ev.Kind)
			}
			want := []string{"tool/result", "interaction/resolved", "step/end"}
			if !e.Cancelled {
				want = append([]string{"diagnostic"}, want...)
			}
			if e.Recovery != nil {
				want = append(want, "runtime/recovery")
			}
			want = append(want, "turn/end")
			if !slices.Equal(kinds, want) {
				t.Fatalf("required closure events changed: %v", kinds)
			}
			var terminal struct{ Status event.TurnStatus }
			if err := json.Unmarshal(events[len(events)-1].Payload, &terminal); err != nil || terminal.Status != status {
				t.Fatalf("terminal status lost: %+v, err=%v", terminal, err)
			}
			if !strings.Contains(logs.String(), "WARN") || !strings.Contains(logs.String(), "discarded optional provider diagnostic") || strings.Contains(logs.String(), "private-peer") {
				t.Fatalf("missing safe warning: %s", logs.String())
			}
		})
	}
}
