package control

import (
	"errors"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/modhost"
)

func TestSavingTheModsSwitchOnIsRefusedUntilItIsAvailable(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	on, off := true, false
	c := &Controller{}

	if got := c.ModsSettings(); got.Available {
		t.Fatalf("settings say available while withheld: %+v", got)
	}
	if _, err := c.SaveModsSettings(ModsPatch{Enabled: &on}); !errors.Is(err, ErrModsWithheld) {
		t.Fatalf("turning on = %v, want ErrModsWithheld", err)
	}
	if config.UserMods().Enabled {
		t.Fatal("a refused save still wrote the switch")
	}
	if _, err := c.SaveModsSettings(ModsPatch{Enabled: &off}); err != nil {
		t.Fatalf("turning off while withheld = %v, want it allowed", err)
	}

	modhost.Complete = true
	t.Cleanup(func() { modhost.Complete = false })
	if _, err := c.SaveModsSettings(ModsPatch{Enabled: &on}); err != nil {
		t.Fatalf("turning on once available = %v", err)
	}
	if !c.ModsSettings().Available || !config.UserMods().Enabled {
		t.Fatal("the switch did not turn on once available")
	}
}
