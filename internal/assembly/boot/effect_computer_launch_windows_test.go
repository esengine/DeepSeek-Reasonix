package boot

import (
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/computer"
	"strings"
	"testing"
)

func TestEffectComputerLaunchKeepsItsApprovalSeparateFromCanonicalRead(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeComputerHelperEnv, "1")
	t.Setenv("REASONIX_BOOT_CUA_INSTALLED_APP", exe)
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_BOOT_CUA_REQUEST_LOG", log)
	driver := computer.NewDriver(exe, os.Environ())
	t.Cleanup(driver.Close)
	reqs := buildBrowserEffect(t, "", []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("apps-1", "computer_read", map[string]any{"what": "apps"})
		},
		func(string) *provider.ToolCall {
			return browserCall("launch-1", "computer_act", map[string]any{"app": exe, "steps": []any{map[string]any{"action": "launch"}}})
		},
	}, Options{ComputerDriver: driver})
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 2 || !strings.Contains(results[0], "installed; no targetable window") || !strings.Contains(results[1], "read separately under its own approval") {
		t.Fatalf("launch did not reach the model: %q", results)
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), `"Name":"launch_app"`)+strings.Count(string(body), `"name":"launch_app"`) != 1 {
		t.Fatalf("native launch missing: %s", body)
	}
	if strings.Contains(string(body), "get_window_state") {
		t.Fatalf("launch borrowed a new read approval: %s", body)
	}
}
