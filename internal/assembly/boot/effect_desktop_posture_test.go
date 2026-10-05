package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

type desktopRun struct {
	dir   string
	ctrl  *control.Controller
	mu    sync.Mutex
	asked []string
}

func (r *desktopRun) prompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// usePostureProvider routes the "boot-posture" provider kind to rec.
func usePostureProvider(rec *postureProvider) {
	postureRegister.Do(func() {
		provider.Register("boot-posture", func(provider.Config) (provider.Provider, error) {
			postureMu.Lock()
			defer postureMu.Unlock()
			return postureCurrent, nil
		})
	})
	postureMu.Lock()
	postureCurrent = rec
	postureMu.Unlock()
}

// buildDesktopRun opens a window's session the way the studio host does and
// declines every approval it is asked for, recording which tools asked.
func buildDesktopRun(t *testing.T, userConfig string, calls ...provider.ToolCall) *desktopRun {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	usePostureProvider(&postureProvider{calls: calls})
	writeUserConfig(t, userConfig)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-posture"
model = "x"
`)
	approveWorkspace(t, dir)
	run := &desktopRun{dir: dir}
	var ref atomic.Pointer[control.Controller]
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.ApprovalRequest {
			return
		}
		run.mu.Lock()
		run.asked = append(run.asked, e.Approval.Tool)
		run.mu.Unlock()
		go ref.Load().Approve(e.Approval.ID, false, false, false)
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink, StatsSource: surface.Desktop})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ref.Store(ctrl)
	ctrl.EnableInteractiveApproval()
	t.Cleanup(func() { ctrl.Close() })
	run.ctrl = ctrl
	return run
}

// A window using the derived posture opens in the same default the terminal
// does, and a trust decision made in the window moves it: a write in
// the workspace lands without asking only where the sandbox confines writes
// and the folder is trusted. Everywhere else the write is put to the person.
func TestEffectDesktopDefaultPostureFollowsSandboxAndTrust(t *testing.T) {
	for _, mode := range []string{"", "workspace-write"} {
		for _, bash := range []string{"off", "enforce"} {
			t.Run(mode+"/"+bash, func(t *testing.T) {
				userConfig := "[sandbox]\nbash = \"" + bash + "\"\n"
				if mode != "" {
					userConfig += "[desktop]\ndefault_tool_approval_mode = \"" + mode + "\"\n"
				}
				run := buildDesktopRun(t, userConfig, writeCall("w", "notes.md"))
				confined := bash == "enforce" && sandbox.Available()
				if got := run.ctrl.WritesConfined(); got != confined {
					t.Fatalf("WritesConfined = %v, want %v", got, confined)
				}
				before := run.ctrl.Posture()
				if !before.Defaulted || before.Trust != config.WorkspaceTrustUndecided || !before.Trustable {
					t.Fatalf("posture on open = %+v, want a defaulted, undecided, trustable folder", before)
				}
				if got := run.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
					t.Fatalf("an undecided folder opens in %q, want ask", got)
				}
				if err := run.ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
					t.Fatal(err)
				}
				want := control.ToolApprovalAsk
				if confined {
					want = control.ToolApprovalAuto
				}
				if got := run.ctrl.ToolApprovalMode(); got != want {
					t.Fatalf("after trusting the folder the session is in %q, want %q", got, want)
				}
				if !run.ctrl.Posture().Defaulted {
					t.Fatal("following the default must not turn it into a named posture")
				}
				_ = run.ctrl.Run(context.Background(), "do the task")
				_, err := os.Stat(filepath.Join(run.dir, "notes.md"))
				if landed := err == nil; landed != confined {
					t.Fatalf("write landed = %v, want %v", landed, confined)
				}
				if asked := len(run.prompts()) > 0; asked == confined {
					t.Fatalf("approval prompts %v; the write is put to the person exactly when it does not land", run.prompts())
				}
			})
		}
	}
}

// A posture the person named is theirs: the trust answer records, and the
// session stays where they put it, in either direction.
func TestEffectDesktopNamedPostureIgnoresTrust(t *testing.T) {
	for _, tc := range []struct {
		written, want string
		trust         config.WorkspaceTrust
	}{
		{control.ToolApprovalAuto, control.ToolApprovalAuto, config.WorkspaceTrustDeclined},
		{control.ToolApprovalAsk, control.ToolApprovalAsk, config.WorkspaceTrusted},
		// A value this build does not know was still written by someone: it
		// opens in Ask and a trust answer does not promote it to Auto.
		{"future-mode", control.ToolApprovalAsk, config.WorkspaceTrusted},
	} {
		t.Run(tc.written, func(t *testing.T) {
			run := buildDesktopRun(t, "[sandbox]\nbash = \"enforce\"\n[desktop]\ndefault_tool_approval_mode = \""+tc.written+"\"\n")
			if got := run.ctrl.ToolApprovalMode(); got != tc.want {
				t.Fatalf("written %q opens in %q, want %q", tc.written, got, tc.want)
			}
			if run.ctrl.Posture().Defaulted {
				t.Fatal("a named posture reads as the default")
			}
			if err := run.ctrl.DecideWorkspaceTrust(tc.trust); err != nil {
				t.Fatal(err)
			}
			if got := run.ctrl.ToolApprovalMode(); got != tc.want {
				t.Fatalf("the trust answer moved a named %q to %q", tc.written, got)
			}
			if got := run.ctrl.Posture().Trust; got != tc.trust {
				t.Fatalf("trust recorded as %q, want %q", got, tc.trust)
			}
		})
	}
}

// A switch on the composer is a named posture: a trust answer after it does
// not take the session back to the default.
func TestEffectDesktopSwitchOutranksLaterTrust(t *testing.T) {
	run := buildDesktopRun(t, "[sandbox]\nbash = \"enforce\"\n")
	run.ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	if err := run.ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := run.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("a named ask became %q after trusting the folder", got)
	}
}

// A home directory is never trusted as a whole, whoever asks.
func TestEffectDesktopRefusesTrustForHome(t *testing.T) {
	isolateConfigHome(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	usePostureProvider(&postureProvider{})
	writeUserConfig(t, `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-posture"
model = "x"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, StatsSource: surface.Desktop})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if ctrl.Posture().Trustable {
		t.Fatal("a home directory reads as trustable")
	}
	if err := ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); !errors.Is(err, control.ErrUntrustableFolder) {
		t.Fatalf("trusting home = %v, want ErrUntrustableFolder", err)
	}
	if got := ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("home opens in %q, want ask", got)
	}
}
