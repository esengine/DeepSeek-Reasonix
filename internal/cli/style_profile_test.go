package cli

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestDetectColorProfileWithoutANSIConsole(t *testing.T) {
	if got := detectColorProfile(false); got != colorprofile.ASCII {
		t.Fatalf("console without VT processing must render plain, got %v", got)
	}
}
