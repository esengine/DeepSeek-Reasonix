package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/session/control"
)

// The posture a person picks on the composer is written to
// `[desktop] default_tool_approval_mode`, and every surface that can write it
// has to read it back — otherwise the setting is write-only and the choice
// looks like it was never made on the next launch.
//
// `serve` is the one that was missing: it is how a phone runs its own kernel,
// and the phone's composer writes the same field the desktop's does. A terminal
// is deliberately still left alone, because it states its posture on its own
// command line and recording that would make a terminal's YOLO the window's
// next default.
func TestWindowPostureAppliesToEveryWindowSurface(t *testing.T) {
	cfg := config.Default()
	cfg.Desktop.DefaultToolApprovalMode = "yolo"

	for _, src := range []surface.Surface{surface.Desktop, surface.Serve, surface.Studio} {
		ctrl := control.New(control.Options{Sink: event.Discard})
		got := withWindowPosture(ctrl, cfg, src, event.Discard)
		if mode := got.ToolApprovalMode(); mode != control.ToolApprovalYolo {
			t.Fatalf("surface %q opened in %q, want the configured yolo", src, mode)
		}
	}

	// A terminal keeps its own posture: the field is a window's, and a CLI
	// writing it would change what the next window opens in.
	cli := control.New(control.Options{Sink: event.Discard})
	if got := withWindowPosture(cli, cfg, surface.CLI, event.Discard); got.ToolApprovalMode() == control.ToolApprovalYolo {
		t.Fatalf("the CLI took the window's posture %q", got.ToolApprovalMode())
	}
}
