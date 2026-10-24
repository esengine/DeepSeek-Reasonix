package builtin

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

func TestBashMandatoryIntegrityCannotUseEscapeApproval(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()
	old := bashSandboxCommand
	bashSandboxCommand = func(_ sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = old }()
	for _, allowed := range []bool{false, true} {
		approver := &fakeSandboxEscapeApprover{allow: true, sessionAllowed: allowed}
		ctx := sandbox.WithEscapeApprover(context.Background(), approver)
		manager := sessiontemp.New()
		manager.Retain()
		t.Cleanup(manager.Release)
		b := bash{sb: sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity}, sessionTemp: manager}
		_, _, lease, err := b.prepareLaunch(ctx, sandbox.ResolveShell("", "", nil), bashParams{Command: "echo never"}, nil)
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != "sandbox.incomplete_integrity" || lease != nil {
			t.Fatalf("mandatory boundary escaped: %v %v", lease, err)
		}
		if len(approver.calls) != 0 || len(approver.sessionChecks) != 0 {
			t.Fatal("environment requirement asked for escape approval")
		}
	}
}
