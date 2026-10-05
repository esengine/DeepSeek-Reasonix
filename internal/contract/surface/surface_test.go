package surface

import "testing"

func TestValidRejectsWhatWasNeverASurface(t *testing.T) {
	for _, known := range []Surface{CLI, Desktop, Studio, Serve, Bot, Remote} {
		if !known.Valid() {
			t.Errorf("%q is declared but not valid", known)
		}
	}
	for _, unknown := range []Surface{"", "CLI", "web", "studio ", "desktop "} {
		if unknown.Valid() {
			t.Errorf("%q was accepted", unknown)
		}
	}
}

// Or is what keeps an unset field from being folded into another surface's
// totals by accident: the caller has to name what it could only have meant.
func TestOrSubstitutesOnlyForWhatIsNotASurface(t *testing.T) {
	if got := Surface("").Or(CLI); got != CLI {
		t.Errorf("empty.Or(CLI) = %q, want %q", got, CLI)
	}
	if got := Surface("studio-preview").Or(CLI); got != CLI {
		t.Errorf("unknown.Or(CLI) = %q, want %q", got, CLI)
	}
	if got := Desktop.Or(CLI); got != Desktop {
		t.Errorf("Desktop.Or(CLI) = %q, want %q", got, Desktop)
	}
}
