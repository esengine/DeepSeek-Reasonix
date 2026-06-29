package builtin

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestPreservedBackgroundWaitDelayIsNonFatal(t *testing.T) {
	if !preservedBackgroundWaitDelay(true, fmt.Errorf("wrapped: %w", exec.ErrWaitDelay)) {
		t.Fatal("preserved background process should treat exec.ErrWaitDelay as non-fatal")
	}
	if preservedBackgroundWaitDelay(false, exec.ErrWaitDelay) {
		t.Fatal("foreground process without preservation should keep exec.ErrWaitDelay fatal")
	}
	if preservedBackgroundWaitDelay(true, fmt.Errorf("other error")) {
		t.Fatal("only exec.ErrWaitDelay should be ignored")
	}
}

func TestBashSchemaMentionsSessionLaunchers(t *testing.T) {
	schema := string((bash{}).Schema())
	for _, want := range []string{"preserve_background_processes", "session launchers", "playwright-cli open"} {
		if !strings.Contains(schema, want) {
			t.Fatalf("bash schema missing %q:\n%s", want, schema)
		}
	}
}
