package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// errYoloNotConfirmed is the person answering no to the one-time YOLO notice.
var errYoloNotConfirmed = errors.New("YOLO was not confirmed; start again without --yolo, or answer y to enable it")

// postureHost is what settling a terminal session's posture reads and writes.
type postureHost interface {
	WritesConfined() bool
	WorkspaceRoot() string
	WorkspaceTrust() config.WorkspaceTrust
	SetWorkspaceTrust(config.WorkspaceTrust) error
	DefaultApprovalMode() string
	SetToolApprovalMode(string)
}

// settleTUIPosture puts an interactive session in the posture it opens in.
// A named YOLO needs the one-time confirmation; an unnamed posture is the
// default, after asking once whether to trust a folder whose writes the
// sandbox would confine.
func settleTUIPosture(ctrl postureHost, mode cliPermissionMode, named bool, home string, in *bufio.Scanner, out io.Writer) error {
	if named {
		if mode.approval == control.ToolApprovalYolo && !config.YoloAcknowledged(home) {
			if err := confirmYolo(home, in, out); err != nil {
				return err
			}
		}
		ctrl.SetToolApprovalMode(mode.approval)
		return nil
	}
	if ctrl.WritesConfined() && ctrl.WorkspaceTrust() == config.WorkspaceTrustUndecided && control.TrustableFolder(ctrl.WorkspaceRoot()) {
		askWorkspaceTrust(ctrl, in, out)
	}
	ctrl.SetToolApprovalMode(ctrl.DefaultApprovalMode())
	return nil
}

func confirmYolo(home string, in *bufio.Scanner, out io.Writer) error {
	fmt.Fprintln(out, "YOLO runs file edits and shell commands without asking first.")
	fmt.Fprintln(out, "The sandbox, network policy and deny rules still apply. This is asked once.")
	if !strings.EqualFold(ask(in, out, "Enable YOLO?", "y/N"), "y") {
		return errYoloNotConfirmed
	}
	if err := config.AcknowledgeYolo(home); err != nil {
		fmt.Fprintln(out, "warning: could not record the confirmation:", err)
	}
	return nil
}

// trustScope is what the sandbox confines a trusted folder's commands to.
const trustScope = `The OS sandbox limits their writes to this folder, your allow_write and --add-dir directories,
temp, and toolchain caches (~/go, ~/.cargo, ~/.cache and the like) — and what lands in a
cache such as ~/.cargo/bin or ~/go/bin runs later outside the sandbox. Network access
follows [sandbox] network; deny rules still apply.`

// askWorkspaceTrust records the answer either way: a folder declined once is
// not asked about again, and `reasonix trust` changes it later.
func askWorkspaceTrust(ctrl postureHost, in *bufio.Scanner, out io.Writer) {
	fmt.Fprintf(out, "In a trusted folder, edits and shell commands run without asking, here and in `reasonix run`:\n%s\n", ctrl.WorkspaceRoot())
	fmt.Fprintln(out, trustScope)
	trust := config.WorkspaceTrustDeclined
	if strings.EqualFold(ask(in, out, "Trust this folder? (`reasonix trust --revoke` undoes it)", "y/N"), "y") {
		trust = config.WorkspaceTrusted
	}
	if err := ctrl.SetWorkspaceTrust(trust); err != nil {
		fmt.Fprintln(out, "warning: could not record the answer:", err)
	}
}
