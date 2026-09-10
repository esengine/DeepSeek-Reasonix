package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The ordinary suite skips external checkout work. Release acceptance sets
// REASONIX_V1382_CHECKOUT to an isolated checkout of the immutable v1.38.2 tag.
func TestOpenCodeGoV10OldReaderRoundTrip(t *testing.T) {
	checkout := os.Getenv("REASONIX_V1382_CHECKOUT")
	if checkout == "" {
		t.Skip("set REASONIX_V1382_CHECKOUT for the actual old-reader acceptance gate")
	}
	var err error
	checkout, err = filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = checkout
	sha, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(sha)) != "f5745bae24a56578e68a81f13e89f09961782d33" {
		t.Fatalf("checkout must be v1.38.2: %q %v", sha, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "old_reader_test.go")
	const source = `package config
import ("os"; "testing")
func TestV1382OpenCodeRoundTripProbe(t *testing.T) {
 path := os.Getenv("REASONIX_COMPAT_CONFIG")
 c := LoadForEdit(path)
 if len(c.Providers) != 5 { t.Fatalf("old reader lost models: %d", len(c.Providers)) }
 p,ok := c.Provider("go"); if !ok { t.Fatal("missing source connection") }; p.DisplayName = "old-reader-edited"
 if os.Getenv("REASONIX_COMPAT_WRITE_V9") == "1" { c.ConfigVersion = 9 }
 if err := c.SaveToScope(path,RenderScopeFull); err != nil { t.Fatal(err) }
}`
	if err := os.WriteFile(probe, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "overlay.json")
	data, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(checkout, "internal/config/opencode_v1382_probe_test.go"): probe}})
	if err := os.WriteFile(overlay, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0", "1"} {
		cmd := exec.Command("go", "test", "-overlay", overlay, "./internal/config", "-run", "^TestV1382OpenCodeRoundTripProbe$", "-v", "-count=1")
		cmd.Dir = checkout
		cmd.Env = append(os.Environ(), "REASONIX_COMPAT_CONFIG="+path, "REASONIX_COMPAT_WRITE_V9="+version)
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "--- PASS: TestV1382OpenCodeRoundTripProbe") {
			t.Fatalf("v1.38.2 reader/save did not pass its probe: %v\n%s", err, output)
		}
		if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
			t.Fatal(err)
		}
		c := LoadForEdit(path)
		p, ok := c.Provider("go")
		if c.ConfigVersion != 10 || len(c.Providers) != 5 || !ok || p.DisplayName != "old-reader-edited" {
			t.Fatalf("round-trip lost configuration: version=%d providers=%d old_output=%s", c.ConfigVersion, len(c.Providers), output)
		}
		if e, ok := c.ResolveModel("go/deepseek-v4-pro"); !ok || e.Name != "go-chat-2" || e.APIKeyEnv != "ACCOUNT_A_KEY" {
			t.Fatalf("old save lost account/history mapping: %+v", e)
		}
		if target, err := c.resolveOpenCodeGoAlias("go/deepseek-v4-flash", true); err != nil || target != "go-search/deepseek-v4-flash" {
			t.Fatalf("old save lost search mapping: %s %v", target, err)
		}
	}
}
