// window_posture.go — the Ask/Auto/YOLO posture a window opens in.
package boot

import (
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/session/control"
)

// withWindowPosture hands back the controller in the posture its config names,
// or, when the person never named one, in the default the sandbox claim and
// folder trust decide. That posture is configuration, not something each shell
// repeats for itself. A terminal frontend states its own and is left alone.
func withWindowPosture(ctrl *control.Controller, cfg *config.Config, src surface.Surface, sink event.Sink) *control.Controller {
	if ctrl == nil || cfg == nil || src != surface.Desktop {
		return ctrl
	}
	if raw := cfg.UnrecognizedDesktopToolApprovalMode(); raw != "" {
		report(sink, event.Event{Level: event.LevelWarn, Code: event.NoticeCodeApprovalModeUnrecognized,
			Text:   "The config's default approval mode is not one this version knows, so new sessions ask before each tool call.",
			Detail: fmt.Sprintf("[desktop] default_tool_approval_mode = %q is not ask, auto or yolo. Choosing a mode replaces it; until then the file keeps it as written.", raw)})
	}
	// An unknown value stays named and normalises to Ask; legacy workspace-write
	// instead follows the sandbox and trust default.
	if mode, ok := control.ParseToolApprovalMode(cfg.DesktopDefaultToolApprovalMode()); ok {
		ctrl.SetToolApprovalMode(mode)
		return ctrl
	}
	ctrl.ApplyDefaultPosture()
	return ctrl
}
