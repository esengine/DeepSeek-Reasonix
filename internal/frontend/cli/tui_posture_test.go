package cli

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

type fakePostureHost struct {
	confined bool
	root     string
	trust    config.WorkspaceTrust
	mode     string
	sets     int
}

func (h *fakePostureHost) WritesConfined() bool                  { return h.confined }
func (h *fakePostureHost) WorkspaceRoot() string                 { return h.root }
func (h *fakePostureHost) WorkspaceTrust() config.WorkspaceTrust { return h.trust }
func (h *fakePostureHost) SetWorkspaceTrust(t config.WorkspaceTrust) error {
	h.trust = t
	return nil
}
func (h *fakePostureHost) DefaultApprovalMode() string {
	return control.DefaultApprovalMode(h.confined, h.trust)
}
func (h *fakePostureHost) SetToolApprovalMode(mode string) { h.mode, h.sets = mode, h.sets+1 }

func settleWith(t *testing.T, h *fakePostureHost, mode cliPermissionMode, named bool, home, input string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := settleTUIPosture(h, mode, named, home, bufio.NewScanner(strings.NewReader(input)), &out)
	return out.String(), err
}

func TestTUIDefaultPostureAsksAboutTrustOnlyWhereWritesAreConfined(t *testing.T) {
	home := testenv.TempDir(t)
	project := testenv.TempDir(t)

	unconfined := &fakePostureHost{root: project}
	if out, _ := settleWith(t, unconfined, cliPermissionMode{}, false, home, "y\n"); out != "" || unconfined.mode != control.ToolApprovalAsk {
		t.Fatalf("without a sandbox the session asks and no trust question is put: mode %q, out %q", unconfined.mode, out)
	}
	if unconfined.trust != config.WorkspaceTrustUndecided {
		t.Fatal("a trust answer was recorded where nobody was asked")
	}

	trusting := &fakePostureHost{confined: true, root: project}
	out, _ := settleWith(t, trusting, cliPermissionMode{}, false, home, "y\n")
	if !strings.Contains(out, "Trust this folder?") || trusting.trust != config.WorkspaceTrusted || trusting.mode != control.ToolApprovalAuto {
		t.Fatalf("a confined, newly trusted folder writes without asking: mode %q trust %q out %q", trusting.mode, trusting.trust, out)
	}

	declining := &fakePostureHost{confined: true, root: project}
	settleWith(t, declining, cliPermissionMode{}, false, home, "\n")
	if declining.trust != config.WorkspaceTrustDeclined || declining.mode != control.ToolApprovalAsk {
		t.Fatalf("an empty answer declines and is remembered: trust %q mode %q", declining.trust, declining.mode)
	}
	if out, _ := settleWith(t, declining, cliPermissionMode{}, false, home, "y\n"); out != "" {
		t.Fatalf("a declined folder is not asked again: %q", out)
	}

	for _, root := range []string{string(filepath.Separator), homeDir(t)} {
		h := &fakePostureHost{confined: true, root: root}
		if out, _ := settleWith(t, h, cliPermissionMode{}, false, home, "y\n"); out != "" || h.mode != control.ToolApprovalAsk {
			t.Fatalf("%s is never offered wholesale trust: mode %q out %q", root, h.mode, out)
		}
	}
}

func TestTUINamedPostureWinsAndYoloIsConfirmedOnce(t *testing.T) {
	home := testenv.TempDir(t)
	h := &fakePostureHost{confined: true, root: testenv.TempDir(t)}
	if out, _ := settleWith(t, h, cliPermissionMode{approval: control.ToolApprovalReadOnly}, true, home, "y\n"); out != "" || h.mode != control.ToolApprovalReadOnly {
		t.Fatalf("a named posture is taken as named: mode %q out %q", h.mode, out)
	}
	yolo := cliPermissionMode{approval: control.ToolApprovalYolo}
	h = &fakePostureHost{}
	if _, err := settleWith(t, h, yolo, true, home, "n\n"); !errors.Is(err, errYoloNotConfirmed) || h.sets != 0 {
		t.Fatalf("declining the YOLO notice starts nothing: err %v, sets %d", err, h.sets)
	}
	if config.YoloAcknowledged(home) {
		t.Fatal("a declined notice was recorded as accepted")
	}
	out, err := settleWith(t, h, yolo, true, home, "y\n")
	if err != nil || h.mode != control.ToolApprovalYolo || !strings.Contains(out, "sandbox, network policy and deny rules still apply") {
		t.Fatalf("accepting enables YOLO and says what still holds: err %v mode %q out %q", err, h.mode, out)
	}
	if !config.YoloAcknowledged(home) {
		t.Fatal("the accepted notice was not recorded")
	}
	if out, _ := settleWith(t, h, yolo, true, home, ""); out != "" {
		t.Fatalf("a confirmed YOLO is not asked about again: %q", out)
	}
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home:", err)
	}
	return home
}
