package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestCheckInputsBindActualBytesWithoutPersistingSecrets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	observer := NewCheckInputObserver(root)
	launch := tool.ShellLaunch{Directory: root, Executable: executable, Interpreter: executable,
		Arguments: []string{executable, "secret-command-value"}, Environment: []string{"TOKEN=secret-token-value"}}
	finish, err := observer.Begin(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	finish(true)
	finish(false) // duplicated completion cannot invalidate an established receipt.
	inputs := observer.Inputs()
	if len(inputs) != 1 || !inputs[0].Completed || inputs[0].Stable || !inputs[0].Before.Complete || !inputs[0].After.Complete {
		t.Fatalf("modified check inputs: %+v", inputs)
	}
	if inputs[0].Before.Digest == inputs[0].After.Digest || len(inputs[0].Gaps) == 0 {
		t.Fatal("content drift or external coverage gap was hidden")
	}
	raw, err := json.Marshal(inputs)
	if err != nil || strings.Contains(string(raw), "secret-") {
		t.Fatalf("raw command/environment persisted: %s, %v", raw, err)
	}
	inputs[0].Gaps[0] = "changed-by-reader"
	if observer.Inputs()[0].Gaps[0] == "changed-by-reader" {
		t.Fatal("receipt aliases mutable observation state")
	}
}

func TestCheckInputsRequirePhysicalCompletionAndExecutableIdentity(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, completed := range []bool{false, true} {
		observer := NewCheckInputObserver(root)
		finish, err := observer.Begin(t.Context(), tool.ShellLaunch{Directory: root, Executable: executable, Interpreter: executable})
		if err != nil {
			t.Fatal(err)
		}
		finish(completed)
		input := observer.Inputs()[0]
		if input.Stable != completed || input.Completed != completed {
			t.Fatalf("completion=%v: %+v", completed, input)
		}
	}
	observer := NewCheckInputObserver(root)
	finish, _ := observer.Begin(t.Context(), tool.ShellLaunch{Directory: root, Executable: "absent", Interpreter: executable})
	finish(true)
	if observer.Inputs()[0].Stable {
		t.Fatal("unresolved executable accepted as a stable launch")
	}
}
