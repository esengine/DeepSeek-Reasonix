package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestTrustApprovesWhatTheWorkspaceNamesOnlyWhenAsked(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "settings.json"), []byte(`{"hooks":{"Stop":[{"command":"echo stop"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rg := filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "opt", "reasonix-test", "rg")
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[tools.search]\nrg_path = '"+rg+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(input string, interactive bool, args ...string) (int, string) {
		var out bytes.Buffer
		code := runTrust(append([]string{"--dir", root}, args...), bufio.NewScanner(strings.NewReader(input)), &out, interactive)
		return code, out.String()
	}

	if code, out := run("", false); code != 1 || !strings.Contains(out, "echo stop") || !strings.Contains(out, rg) {
		t.Fatalf("non-interactive listing = %d %q, want both programs listed and nothing approved", code, out)
	}
	if code, _ := run("n\n", true); code != 1 {
		t.Fatalf("declined prompt exit = %d, want 1", code)
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 2 {
		t.Fatalf("pending after declining = %d, want 2", len(pending))
	}
	if code, out := run("y\n", true); code != 0 {
		t.Fatalf("approving = %d %q", code, out)
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 0 {
		t.Fatalf("pending after approval = %+v, want none", pending)
	}
	if code, _ := run("", false, "--revoke"); code != 0 {
		t.Fatal("revoke failed")
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 2 {
		t.Fatalf("pending after revoke = %d, want 2", len(pending))
	}
}
