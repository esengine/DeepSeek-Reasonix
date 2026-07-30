package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The shipped example config must stay parseable, and its commented
// [tools.powershell] block must leave the tool disabled — the example is the
// reference users copy from, so a drift here either breaks first-run configs
// or silently changes the default tool list.
func TestExampleTOMLParsesWithPowerShellDisabled(t *testing.T) {
	var c Config
	if _, err := toml.DecodeFile(filepath.Join("..", "..", "reasonix.example.toml"), &c); err != nil {
		t.Fatalf("reasonix.example.toml does not parse: %v", err)
	}
	if c.PowerShellToolEnabled() {
		t.Fatal("reasonix.example.toml must keep the powershell tool disabled by default")
	}
}

func TestPowerShellToolEnabledDefault(t *testing.T) {
	tru, fls := true, false
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil config", nil, false},
		{"zero config", &Config{}, false},
		{"nil enabled", &Config{Tools: ToolsConfig{PowerShell: PowerShellConfig{Prefer: "pwsh"}}}, false},
		{"explicit false", &Config{Tools: ToolsConfig{PowerShell: PowerShellConfig{Enabled: &fls}}}, false},
		{"explicit true", &Config{Tools: ToolsConfig{PowerShell: PowerShellConfig{Enabled: &tru}}}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.PowerShellToolEnabled(); got != tc.want {
			t.Errorf("%s: PowerShellToolEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPowerShellConfigParses(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want PowerShellConfig
	}{
		{
			name: "enabled true",
			toml: "[tools.powershell]\nenabled = true\n",
			want: PowerShellConfig{Enabled: boolPtr(true)},
		},
		{
			name: "full block",
			toml: "[tools.powershell]\nenabled = true\nprefer = \"powershell\"\npath = \"C:\\\\pwsh.exe\"\n",
			want: PowerShellConfig{Enabled: boolPtr(true), Prefer: "powershell", Path: `C:\pwsh.exe`},
		},
		{
			name: "absent section",
			toml: "[tools]\nbash_timeout_seconds = 60\n",
			want: PowerShellConfig{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c Config
			if _, err := toml.Decode(tc.toml, &c); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := c.Tools.PowerShell
			if (got.Enabled == nil) != (tc.want.Enabled == nil) {
				t.Fatalf("Enabled = %v, want %v", got.Enabled, tc.want.Enabled)
			}
			if got.Enabled != nil && *got.Enabled != *tc.want.Enabled {
				t.Fatalf("Enabled = %v, want %v", *got.Enabled, *tc.want.Enabled)
			}
			if got.Prefer != tc.want.Prefer || got.Path != tc.want.Path {
				t.Fatalf("PowerShell = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRenderTOMLPowerShellRoundTrip(t *testing.T) {
	orig := Default()
	orig.Tools.PowerShell.Enabled = boolPtr(true)
	orig.Tools.PowerShell.Prefer = "powershell"
	orig.Tools.PowerShell.Path = `C:\tools\pwsh.exe`

	rendered := RenderTOML(orig)
	for _, want := range []string{"[tools.powershell]", "enabled = true", `prefer = "powershell"`, `path   = "C:\\tools\\pwsh.exe"`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q", want)
		}
	}

	got := Default()
	if _, err := toml.Decode(rendered, got); err != nil {
		t.Fatalf("re-parse rendered config: %v", err)
	}
	if !got.PowerShellToolEnabled() {
		t.Fatalf("round-trip lost enabled=true: %+v", got.Tools.PowerShell)
	}
	if got.Tools.PowerShell.Prefer != "powershell" || got.Tools.PowerShell.Path != `C:\tools\pwsh.exe` {
		t.Fatalf("round-trip PowerShell = %+v", got.Tools.PowerShell)
	}
}

func TestRenderTOMLPowerShellDefaultOmitsBlock(t *testing.T) {
	rendered := RenderTOML(Default())
	if strings.Contains(rendered, "[tools.powershell]") {
		t.Fatalf("default config must not render [tools.powershell] (tool list byte-stability)")
	}

	// Explicitly disabled also renders the block (it was explicitly set) but
	// keeps the gate off after re-parse.
	c := Default()
	c.Tools.PowerShell.Enabled = boolPtr(false)
	got := Default()
	if _, err := toml.Decode(RenderTOML(c), got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got.PowerShellToolEnabled() {
		t.Fatal("enabled=false must keep the tool disabled after round-trip")
	}
}

func TestProjectDeltaRendersPowerShellOverrides(t *testing.T) {
	c := Default()
	c.Tools.PowerShell.Enabled = boolPtr(true)

	delta := RenderTOMLProjectDelta(c)
	for _, want := range []string{"[tools.powershell]", "enabled = true"} {
		if !strings.Contains(delta, want) {
			t.Fatalf("project delta missing %q:\n%s", want, delta)
		}
	}

	got := Default()
	if _, err := toml.Decode(delta, got); err != nil {
		t.Fatalf("decode project delta: %v\n%s", err, delta)
	}
	if !got.PowerShellToolEnabled() {
		t.Fatalf("project delta lost powershell enabled: %+v", got.Tools.PowerShell)
	}
}

func TestProjectDeltaOmitsPowerShellByDefault(t *testing.T) {
	delta := RenderTOMLProjectDelta(Default())
	if strings.Contains(delta, "[tools.powershell]") {
		t.Fatalf("default project delta must not emit [tools.powershell]:\n%s", delta)
	}
}
