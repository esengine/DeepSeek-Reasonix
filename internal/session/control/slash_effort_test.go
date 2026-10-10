package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
)

// A model that cannot switch thinking off still reasons at its cheapest level,
// and still bills for it. The /effort menu is where that level is picked, so it
// is where that has to be said — otherwise the rung reads as "off".
const effortSeedConfig = `config_version = 6
default_model = "glm/glm-5.3"

[[providers]]
name        = "glm"
kind        = "openai"
base_url    = "https://api.z.ai/api/paas/v4"
models      = ["glm-5.3", "glm-5.2"]
default     = "glm-5.3"
api_key_env = "ZAI_API_KEY"
`

func seedEffortConfig(t *testing.T) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(effortSeedConfig), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func hintOf(items []SlashItem, label string) string {
	for _, it := range items {
		if it.Label == label {
			return it.Hint
		}
	}
	return ""
}

func TestSlashEffortMarksTheForcedThinkingRung(t *testing.T) {
	seedEffortConfig(t)
	note := i18n.M.ArgEffortForcedOn

	items, _ := SlashArgItems("/effort ", ArgData{CurrentModel: "glm/glm-5.3"})
	if !has(items, "low") || !has(items, "max") {
		t.Fatalf("/effort should offer the GLM-5.3 ladder; got %v", labelsOf(items))
	}
	if !strings.Contains(hintOf(items, "low"), note) {
		t.Errorf("low hint = %q, want it to carry %q", hintOf(items, "low"), note)
	}
	for _, level := range []string{"auto", "high", "max"} {
		if strings.Contains(hintOf(items, level), note) {
			t.Errorf("%s hint = %q, want no forced-thinking note", level, hintOf(items, level))
		}
	}

	// GLM-5.2 can switch thinking off, so nothing in its menu claims otherwise.
	items, _ = SlashArgItems("/effort ", ArgData{CurrentModel: "glm/glm-5.2"})
	for _, it := range items {
		if strings.Contains(it.Hint, note) {
			t.Errorf("glm-5.2 %s hint = %q, want no forced-thinking note", it.Label, it.Hint)
		}
	}
}

func TestEffortBareReportsForcedThinkingAndBilling(t *testing.T) {
	c, take := settingsController(t)
	seedUserConfig(t, effortSeedConfig+"effort = \"disabled\"\n")
	for _, lang := range []string{"en", "zh", "zh-TW"} {
		i18n.DetectLanguage(lang)
		for _, ref := range []string{"glm/glm-5.3", "glm/glm-5.2"} {
			c.modelRef = ref
			c.managementNotice("/effort")
			got := lastNotice(t, take())
			forced := ref == "glm/glm-5.3"
			if strings.Contains(got, i18n.M.ArgEffortForcedOn) != forced {
				t.Errorf("%s: notice = %q, forced-thinking warning wanted %v", ref, got, forced)
			}
			if forced && !strings.Contains(got, fmt.Sprintf(i18n.M.EffortStatusFmt, "glm", "low", "max", "auto|low|high|max")) {
				t.Errorf("stored disabled on GLM-5.3 was not displayed as low: %q", got)
			}
		}
	}
}

const responsesRelaySeed = `config_version = 6
default_model = "relay/gpt-5.5"

[[providers]]
name        = "relay"
kind        = "responses"
base_url    = "https://www.dmxapi.cn/v1"
models      = ["gpt-5.5"]
default     = "gpt-5.5"
api_key_env = "RELAY_API_KEY"
`

func TestEffortOnAResponsesEntryFollowsItsDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name, extra, want string
	}{
		{"declared levels", "supported_efforts = [\"low\", \"high\", \"xhigh\"]\n", "auto|low|high|xhigh"},
		{"declared protocol", "reasoning_protocol = \"openai\"\n", "auto|low|medium|high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.DetectLanguage("en")
			c, take := settingsController(t)
			seedUserConfig(t, responsesRelaySeed+tc.extra)
			c.modelRef = "relay/gpt-5.5"
			c.managementNotice("/effort")
			got := lastNotice(t, take())
			if !strings.Contains(got, tc.want) {
				t.Errorf("/effort = %q, want the ladder %s", got, tc.want)
			}
			items, _ := SlashArgItems("/effort ", ArgData{CurrentModel: "relay/gpt-5.5"})
			if len(items) < 2 {
				t.Errorf("/effort argument menu = %v, want the declared levels", labelsOf(items))
			}
		})
	}
}

func TestEffortOnAnUndeclaredResponsesEntryIsReportedNotConfigurable(t *testing.T) {
	i18n.DetectLanguage("en")
	c, take := settingsController(t)
	seedUserConfig(t, responsesRelaySeed)
	c.modelRef = "relay/gpt-5.5"
	c.managementNotice("/effort")
	got := lastNotice(t, take())
	if want := fmt.Sprintf(i18n.M.EffortUnsupportedFmt, "relay"); got != want {
		t.Errorf("/effort = %q, want %q", got, want)
	}
}
