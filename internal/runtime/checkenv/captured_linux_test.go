//go:build linux

package checkenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/shellrun"
)

func TestLinuxCapturedReceiptCannotAuthorizeOrdinaryIntegration(t *testing.T) {
	spec := runtimeFixture(t)
	path := filepath.Join(spec.SourceRoot, "capture_test.go")
	body := []byte("package p\nimport \"testing\"\nfunc TestCaptured(t *testing.T){}\n")
	if err := os.WriteFile(path, []byte("rewritten"), 0600); err != nil {
		t.Fatal(err)
	}
	command := `[[ $(< capture_test.go) == 'package p
import "testing"
func TestCaptured(t *testing.T){}' ]] || exit 11
printf '%s\n' '{"Action":"pass","Test":"TestCaptured"}'`
	i := New(spec.WithCaptured(path, body), command, "captured-execution", "captured-call")
	defer i.Close()
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true})
	if result.Err != nil {
		t.Fatalf("capture launch: %v %s", result.Err, result.Combined)
	}
	inputs := i.Inputs()
	if len(inputs) != 1 || !VerifyCaptured(inputs[0], "captured-execution", path, body) || i.CapturedOutcome() != evidence.CriterionPass {
		t.Fatalf("no captured receipt: %+v, outcome=%s", inputs, i.CapturedOutcome())
	}
	if Verify(inputs[0], "captured-execution") || VerifyCaptured(inputs[0], "other", path, body) || VerifyCaptured(inputs[0], "captured-execution", path, []byte("other")) {
		t.Fatal("captured receipt crossed purpose or identity")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "rewritten" {
		t.Fatalf("source overwritten: %q %v", raw, err)
	}
}
