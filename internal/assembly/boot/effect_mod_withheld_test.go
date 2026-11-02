package boot

import (
	"strings"
	"testing"

	"reasonix/internal/ext/modhost"
)

// Until the switch is available a mod never runs, whatever the user file says,
// and its status names why.
func TestEffectAModStaysOffWhileTheSwitchIsWithheldEvenIfTheFileSaysOn(t *testing.T) {
	_, _, prov := installMod(t, denyingMod)
	modhost.Complete = false
	setModsSwitch(t, true)

	runModTurn(t, prov, "echo FORBIDDEN-ran")
	got := strings.Join(prov.toolResults(), "\n")
	if strings.Contains(got, "mod says") || !strings.Contains(got, "FORBIDDEN-ran") {
		t.Fatalf("a withheld mod still ruled on the call:\n%s", got)
	}
	if st := modhost.Default.Snapshot(); len(st) != 1 || st[0].State != modhost.StateInactive || st[0].Code != modhost.CodeWithheld {
		t.Fatalf("status = %+v, want inactive with %s", st, modhost.CodeWithheld)
	}
}
