//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/UserExistsError/conpty"
)

func TestAWindowsWithoutAPseudoConsoleIsAnUnsupportedTerminalNotAFailedStart(t *testing.T) {
	err := ptyStartError(fmt.Errorf("start: %w", conpty.ErrConPtyUnsupported))
	if CodeOf(err) != CodeUnsupported {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeUnsupported)
	}
}

func TestAnyOtherStartFailureStaysAFailedStart(t *testing.T) {
	if got := CodeOf(ptyStartError(errors.New("the file cannot be found"))); got != CodeStartFailed {
		t.Fatalf("code = %q, want %q", got, CodeStartFailed)
	}
}
