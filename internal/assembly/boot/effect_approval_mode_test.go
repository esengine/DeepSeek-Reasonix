package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

// A default approval mode this build does not know opens the window in Ask and
// says so with a code the frontend localizes; a known one says nothing.
func TestEffectUnrecognizedApprovalModeOpensInAskWithANotice(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		want   string
		notice bool
	}{
		{"future-mode", control.ToolApprovalAsk, true},
		{"read-only", control.ToolApprovalAsk, false},
		{"danger-full-access", control.ToolApprovalYolo, false},
		{"workspace-write", control.ToolApprovalAsk, false},
		{"auto", control.ToolApprovalAuto, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			isolateConfigHome(t)
			t.Chdir(robustTempDir(t))
			writeUserConfig(t, userModel+"\n[desktop]\ndefault_tool_approval_mode = \""+tc.mode+"\"\n")
			registerBootTokenProfileTestProvider()
			setBootTokenProfileTestProvider(t, testutil.NewMock("posture"))
			var notices []event.Event
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Notice && e.Code == event.NoticeCodeApprovalModeUnrecognized {
					notices = append(notices, e)
				}
			})
			ctrl, err := Build(context.Background(), Options{Sink: sink, StatsSource: surface.Desktop})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			if got := ctrl.ToolApprovalMode(); got != tc.want {
				t.Fatalf("approval mode = %q, want %q", got, tc.want)
			}
			if got := len(notices) == 1; got != tc.notice {
				t.Fatalf("approval-mode notices = %v, want one: %v", notices, tc.notice)
			}
		})
	}
}
