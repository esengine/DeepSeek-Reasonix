package control

import (
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
)

// A frontend that routes raw input through Submit gets /preset, its aliases
// and /remote answered, not "unknown command".
func TestPresetAndRemoteAreAnsweredOnSubmitPath(t *testing.T) {
	c, take := settingsController(t)
	for _, line := range []string{"/preset", "/work-mode", "/profile", "/remote"} {
		c.Submit(line)
		got := take()
		if len(got) == 0 || strings.Contains(strings.Join(got, "\n"), "unknown command") {
			t.Errorf("%s was not answered: %q", line, got)
		}
	}
}

func TestPresetSetsTheRoleForTheSession(t *testing.T) {
	c, take := settingsController(t)
	c.Submit("/preset delivery")
	if got := c.AgentPreset(); got != "delivery" {
		t.Fatalf("preset after /preset delivery = %q", got)
	}
	if n := lastNotice(t, take()); !strings.Contains(n, "delivery") {
		t.Fatalf("notice = %q", n)
	}
	c.Submit("/preset nonsense")
	if got := c.AgentPreset(); got != "delivery" {
		t.Fatalf("an unknown value changed the preset to %q", got)
	}
	if n := lastNotice(t, take()); n != i18n.M.WorkModeUsage {
		t.Fatalf("notice = %q, want usage", n)
	}
}

func TestPresetAliasWarnsItIsDeprecated(t *testing.T) {
	c, take := settingsController(t)
	c.Submit("/work-mode balanced")
	got := take()
	if len(got) != 2 || got[0] != i18n.M.WorkModeDeprecatedNotice {
		t.Fatalf("notices = %q", got)
	}
}

func TestRemoteListsConfiguredHosts(t *testing.T) {
	c, take := settingsController(t)
	c.Submit("/remote")
	if n := lastNotice(t, take()); n != i18n.M.RemoteNoHostsHint {
		t.Fatalf("empty config notice = %q", n)
	}
	seedUserConfig(t, "[[remote.hosts]]\nname = \"box\"\nhost = \"10.0.0.7\"\nuser = \"dev\"\nport = 2200\n")
	c.Submit("/remote")
	n := lastNotice(t, take())
	if !strings.Contains(n, "box  dev@10.0.0.7:2200") || !strings.Contains(n, "reasonix remote connect") {
		t.Fatalf("notice = %q", n)
	}
}
